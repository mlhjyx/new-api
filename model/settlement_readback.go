package model

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	SettlementReadbackCredentialActive  = 1
	SettlementReadbackCredentialRevoked = 2

	SettlementReadbackBindingBound           = "BOUND"
	SettlementReadbackBindingDispatchStarted = "DISPATCH_STARTED"
	SettlementReadbackBindingLogLinked       = "LOG_LINKED"

	settlementReadbackCredentialPrefix             = "srb1"
	settlementReadbackSecretBytes                  = 32
	settlementReadbackLookupBytes                  = 12
	settlementReadbackDigestHexLength              = sha256.Size * 2
	settlementReadbackLookupPrefixLength           = 16
	settlementReadbackSecretPartLength             = 43
	settlementReadbackMaximumPepperKeys            = 3
	settlementReadbackMaximumKeyringBytes          = 4096
	settlementReadbackMaximumRotationSeconds int64 = 600
)

var errSettlementReadbackCredentialInvalid = errors.New("invalid settlement readback credential")
var settlementReadbackPepperVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)

type SettlementReadbackPepperKeyring struct {
	activeVersion string
	statuses      map[string]string
	peppers       map[string]string
}

func ParseSettlementReadbackPepperKeyring(raw []byte) (*SettlementReadbackPepperKeyring, error) {
	if len(raw) == 0 || len(raw) > settlementReadbackMaximumKeyringBytes || raw[len(raw)-1] != '\n' || strings.ContainsRune(string(raw), '\x00') {
		return nil, errSettlementReadbackCredentialInvalid
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) < 3 || lines[0] != "schema=settlement-readback-pepper-keyring/v1" || lines[len(lines)-1] != "" {
		return nil, errSettlementReadbackCredentialInvalid
	}
	entries := lines[1 : len(lines)-1]
	if len(entries) < 1 || len(entries) > settlementReadbackMaximumPepperKeys {
		return nil, errSettlementReadbackCredentialInvalid
	}
	statuses := make(map[string]string, len(entries))
	peppers := make(map[string]string, len(entries))
	activeVersion := ""
	for _, line := range entries {
		parts := strings.Split(line, " ")
		if len(parts) != 3 || !settlementReadbackPepperVersion.MatchString(parts[0]) || (parts[1] != "ACTIVE" && parts[1] != "VERIFY_ONLY") {
			return nil, errSettlementReadbackCredentialInvalid
		}
		if _, exists := peppers[parts[0]]; exists || len(parts[2]) != settlementReadbackSecretPartLength {
			return nil, errSettlementReadbackCredentialInvalid
		}
		decoded, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil || len(decoded) != settlementReadbackSecretBytes {
			return nil, errSettlementReadbackCredentialInvalid
		}
		if parts[1] == "ACTIVE" {
			if activeVersion != "" {
				return nil, errSettlementReadbackCredentialInvalid
			}
			activeVersion = parts[0]
		}
		statuses[parts[0]] = parts[1]
		peppers[parts[0]] = string(decoded)
	}
	if activeVersion == "" {
		return nil, errSettlementReadbackCredentialInvalid
	}
	return &SettlementReadbackPepperKeyring{activeVersion: activeVersion, statuses: statuses, peppers: peppers}, nil
}

func (keyring *SettlementReadbackPepperKeyring) Active() (string, string, bool) {
	if keyring == nil || keyring.activeVersion == "" {
		return "", "", false
	}
	pepper, ok := keyring.peppers[keyring.activeVersion]
	return keyring.activeVersion, pepper, ok
}

func (keyring *SettlementReadbackPepperKeyring) Lookup(version string) (string, string, bool) {
	if keyring == nil {
		return "", "", false
	}
	status, ok := keyring.statuses[version]
	if !ok {
		return "", "", false
	}
	pepper, ok := keyring.peppers[version]
	return status, pepper, ok
}

// SettlementReadbackCredential is deliberately separate from Token. Its secret
// is issued once and only a peppered digest is stored.
type SettlementReadbackCredential struct {
	Id              int    `json:"id"`
	LookupPrefix    string `json:"lookup_prefix" gorm:"type:varchar(32);uniqueIndex"`
	SecretDigest    string `json:"-" gorm:"type:char(64)"`
	PepperVersion   string `json:"pepper_version" gorm:"type:varchar(64)"`
	DispatchTokenId int    `json:"dispatch_token_id" gorm:"index"`
	Status          int    `json:"status" gorm:"index"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint"`
	RevokedAt       int64  `json:"revoked_at" gorm:"bigint;default:0"`
	RotationEndsAt  int64  `json:"rotation_ends_at" gorm:"bigint;default:0"`
}

// SettlementReadbackBinding is allocated before the first upstream write. It
// stores only request/nonce digests; the plaintext nonce is reconstructible by
// Backend and must never enter the gateway database.
type SettlementReadbackBinding struct {
	Id                        int    `json:"id"`
	DispatchTokenId           int    `json:"dispatch_token_id" gorm:"index;not null;uniqueIndex:idx_settlement_request,priority:1;uniqueIndex:idx_settlement_nonce,priority:1"`
	SettlementRequestIdSha256 string `json:"-" gorm:"type:char(64);not null;uniqueIndex:idx_settlement_request,priority:2"`
	SettlementNonceSha256     string `json:"-" gorm:"type:char(64);not null;uniqueIndex:idx_settlement_nonce,priority:2"`
	GatewayRequestId          string `json:"-" gorm:"type:varchar(64);not null"`
	State                     string `json:"state" gorm:"type:varchar(32);not null;index"`
	CreatedAt                 int64  `json:"created_at" gorm:"bigint;not null"`
	LogLinkedAt               int64  `json:"log_linked_at" gorm:"bigint;not null;default:0"`
}

func (binding *SettlementReadbackBinding) IsDispatchStarted() bool {
	return binding != nil && (binding.State == SettlementReadbackBindingDispatchStarted || binding.State == SettlementReadbackBindingLogLinked)
}

func settlementReadbackDigest(lookupPrefix string, secretPart string, pepper string) string {
	value := sha256.Sum256([]byte(settlementReadbackCredentialPrefix + "\x00" + pepper + "\x00" + lookupPrefix + "\x00" + secretPart))
	return hex.EncodeToString(value[:])
}

func NewSettlementReadbackCredential(dispatchTokenID int, pepperVersion string, pepper string) (*SettlementReadbackCredential, string, error) {
	return newSettlementReadbackCredential(dispatchTokenID, pepperVersion, pepper, time.Now().Unix())
}

func newSettlementReadbackCredential(dispatchTokenID int, pepperVersion string, pepper string, now int64) (*SettlementReadbackCredential, string, error) {
	if dispatchTokenID < 1 || strings.TrimSpace(pepperVersion) == "" || strings.TrimSpace(pepper) == "" {
		return nil, "", errSettlementReadbackCredentialInvalid
	}
	lookupBytes := make([]byte, settlementReadbackLookupBytes)
	secretBytes := make([]byte, settlementReadbackSecretBytes)
	if _, err := rand.Read(lookupBytes); err != nil {
		return nil, "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", err
	}
	lookupPrefix := base64.RawURLEncoding.EncodeToString(lookupBytes)
	if len(lookupPrefix) != settlementReadbackLookupPrefixLength {
		return nil, "", errSettlementReadbackCredentialInvalid
	}
	secretPart := base64.RawURLEncoding.EncodeToString(secretBytes)
	// base64url permits '_' and '-', so use '.' as the credential delimiter.
	secret := settlementReadbackCredentialPrefix + "." + lookupPrefix + "." + secretPart
	return &SettlementReadbackCredential{
		LookupPrefix:    lookupPrefix,
		SecretDigest:    settlementReadbackDigest(lookupPrefix, secretPart, pepper),
		PepperVersion:   pepperVersion,
		DispatchTokenId: dispatchTokenID,
		Status:          SettlementReadbackCredentialActive,
		CreatedAt:       now,
	}, secret, nil
}

func SettlementReadbackCredentialUsableAt(credential *SettlementReadbackCredential, now int64) bool {
	return credential != nil && credential.Status == SettlementReadbackCredentialActive && (credential.RotationEndsAt == 0 || now < credential.RotationEndsAt)
}

func revokeExpiredSettlementReadbackCredentials(tx *gorm.DB, dispatchTokenID int, now int64) error {
	return tx.Model(&SettlementReadbackCredential{}).
		Where("dispatch_token_id = ? AND status = ? AND rotation_ends_at > 0 AND rotation_ends_at <= ?", dispatchTokenID, SettlementReadbackCredentialActive, now).
		Updates(map[string]interface{}{"status": SettlementReadbackCredentialRevoked, "revoked_at": now}).Error
}

func CreateSettlementReadbackCredential(db *gorm.DB, dispatchTokenID int, keyring *SettlementReadbackPepperKeyring, now int64) (*SettlementReadbackCredential, string, error) {
	version, pepper, ok := keyring.Active()
	if db == nil || dispatchTokenID < 1 || !ok || now < 1 {
		return nil, "", errSettlementReadbackCredentialInvalid
	}
	var created *SettlementReadbackCredential
	var secret string
	err := db.Transaction(func(tx *gorm.DB) error {
		var token Token
		if err := lockForUpdate(tx).Where("id = ?", dispatchTokenID).First(&token).Error; err != nil || token.Status != common.TokenStatusEnabled {
			return errSettlementReadbackCredentialInvalid
		}
		if err := revokeExpiredSettlementReadbackCredentials(tx, dispatchTokenID, now); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&SettlementReadbackCredential{}).Where("dispatch_token_id = ? AND status = ?", dispatchTokenID, SettlementReadbackCredentialActive).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return errSettlementReadbackCredentialInvalid
		}
		credential, oneTimeSecret, err := newSettlementReadbackCredential(dispatchTokenID, version, pepper, now)
		if err != nil {
			return err
		}
		if err := tx.Create(credential).Error; err != nil {
			return err
		}
		created, secret = credential, oneTimeSecret
		return nil
	})
	return created, secret, err
}

func RotateSettlementReadbackCredential(db *gorm.DB, credentialID int, overlapSeconds int64, keyring *SettlementReadbackPepperKeyring, now int64) (*SettlementReadbackCredential, string, error) {
	version, pepper, ok := keyring.Active()
	if db == nil || credentialID < 1 || overlapSeconds < 1 || overlapSeconds > settlementReadbackMaximumRotationSeconds || !ok || now < 1 {
		return nil, "", errSettlementReadbackCredentialInvalid
	}
	var created *SettlementReadbackCredential
	var secret string
	err := db.Transaction(func(tx *gorm.DB) error {
		var current SettlementReadbackCredential
		if err := lockForUpdate(tx).Where("id = ?", credentialID).First(&current).Error; err != nil || !SettlementReadbackCredentialUsableAt(&current, now) {
			return errSettlementReadbackCredentialInvalid
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ?", current.DispatchTokenId).First(&token).Error; err != nil || token.Status != common.TokenStatusEnabled {
			return errSettlementReadbackCredentialInvalid
		}
		if err := revokeExpiredSettlementReadbackCredentials(tx, current.DispatchTokenId, now); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&SettlementReadbackCredential{}).Where("dispatch_token_id = ? AND status = ?", current.DispatchTokenId, SettlementReadbackCredentialActive).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errSettlementReadbackCredentialInvalid
		}
		endsAt := now + overlapSeconds
		if result := tx.Model(&SettlementReadbackCredential{}).Where("id = ? AND status = ?", current.Id, SettlementReadbackCredentialActive).Update("rotation_ends_at", endsAt); result.Error != nil || result.RowsAffected != 1 {
			return errSettlementReadbackCredentialInvalid
		}
		credential, oneTimeSecret, err := newSettlementReadbackCredential(current.DispatchTokenId, version, pepper, now)
		if err != nil {
			return err
		}
		if err := tx.Create(credential).Error; err != nil {
			return err
		}
		created, secret = credential, oneTimeSecret
		return nil
	})
	return created, secret, err
}

func RevokeSettlementReadbackCredential(db *gorm.DB, credentialID int, now int64) (bool, error) {
	if db == nil || credentialID < 1 || now < 1 {
		return false, errSettlementReadbackCredentialInvalid
	}
	replay := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var credential SettlementReadbackCredential
		if err := lockForUpdate(tx).Where("id = ?", credentialID).First(&credential).Error; err != nil {
			return err
		}
		if credential.Status == SettlementReadbackCredentialRevoked {
			replay = true
			return nil
		}
		if credential.Status != SettlementReadbackCredentialActive {
			return errSettlementReadbackCredentialInvalid
		}
		result := tx.Model(&SettlementReadbackCredential{}).Where("id = ? AND status = ?", credentialID, SettlementReadbackCredentialActive).Updates(map[string]interface{}{"status": SettlementReadbackCredentialRevoked, "revoked_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			return errSettlementReadbackCredentialInvalid
		}
		return nil
	})
	return replay, err
}

func (credential *SettlementReadbackCredential) MatchesSecret(secret string, pepper string) bool {
	if credential == nil || credential.Status != SettlementReadbackCredentialActive || strings.TrimSpace(pepper) == "" {
		return false
	}
	parts := strings.Split(secret, ".")
	if len(parts) != 3 || parts[0] != settlementReadbackCredentialPrefix || parts[1] != credential.LookupPrefix || len(parts[1]) != settlementReadbackLookupPrefixLength {
		return false
	}
	digest := settlementReadbackDigest(parts[1], parts[2], pepper)
	return subtle.ConstantTimeCompare([]byte(digest), []byte(credential.SecretDigest)) == 1
}

func settlementReadbackCredentialParts(secret string) (string, bool) {
	parts := strings.Split(secret, ".")
	if len(parts) != 3 || parts[0] != settlementReadbackCredentialPrefix || len(parts[1]) != settlementReadbackLookupPrefixLength || parts[1] == "" || len(parts[2]) != settlementReadbackSecretPartLength {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(decoded) != settlementReadbackSecretBytes {
		return "", false
	}
	return parts[1], true
}

func GetActiveSettlementReadbackCredential(secret string, pepper string) (*SettlementReadbackCredential, error) {
	lookupPrefix, ok := settlementReadbackCredentialParts(secret)
	if !ok || strings.TrimSpace(pepper) == "" {
		return nil, errSettlementReadbackCredentialInvalid
	}
	var credential SettlementReadbackCredential
	if err := DB.Where("lookup_prefix = ? AND status = ?", lookupPrefix, SettlementReadbackCredentialActive).First(&credential).Error; err != nil {
		return nil, err
	}
	if !credential.MatchesSecret(secret, pepper) {
		return nil, errSettlementReadbackCredentialInvalid
	}
	return &credential, nil
}

func HasActiveSettlementReadbackCredential(dispatchTokenID int) (bool, error) {
	if dispatchTokenID < 1 {
		return false, errSettlementReadbackCredentialInvalid
	}
	var count int64
	err := DB.Model(&SettlementReadbackCredential{}).
		Where("dispatch_token_id = ? AND status = ?", dispatchTokenID, SettlementReadbackCredentialActive).
		Count(&count).Error
	return count > 0, err
}

func BeginSettlementReadbackDispatch(db *gorm.DB, bindingID int) bool {
	result := db.Model(&SettlementReadbackBinding{}).Where("id = ? AND state = ?", bindingID, SettlementReadbackBindingBound).Update("state", SettlementReadbackBindingDispatchStarted)
	return result.Error == nil && result.RowsAffected == 1
}

func LinkSettlementReadbackConsumeLog(db *gorm.DB, bindingID int, log *Log) error {
	if log == nil || log.Type != LogTypeConsume || log.SettlementBindingId == nil || *log.SettlementBindingId != bindingID {
		return errSettlementReadbackCredentialInvalid
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var binding SettlementReadbackBinding
		if err := tx.First(&binding, bindingID).Error; err != nil {
			return err
		}
		if binding.State != SettlementReadbackBindingDispatchStarted || binding.DispatchTokenId != log.TokenId {
			return errSettlementReadbackCredentialInvalid
		}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		return tx.Model(&SettlementReadbackBinding{}).Where("id = ? AND state = ?", bindingID, SettlementReadbackBindingDispatchStarted).Updates(map[string]interface{}{"state": SettlementReadbackBindingLogLinked, "log_linked_at": time.Now().Unix()}).Error
	})
}

func FindSettlementReadbackConsumeLog(db *gorm.DB, tokenID int, requestDigest string, nonceDigest string) (*Log, bool, error) {
	var binding SettlementReadbackBinding
	if err := db.Where("dispatch_token_id = ? AND settlement_request_id_sha256 = ? AND settlement_nonce_sha256 = ?", tokenID, requestDigest, nonceDigest).First(&binding).Error; err != nil {
		return nil, false, err
	}
	if binding.State == SettlementReadbackBindingDispatchStarted {
		return nil, true, nil
	}
	if binding.State != SettlementReadbackBindingLogLinked {
		return nil, false, errSettlementReadbackCredentialInvalid
	}
	var log Log
	if err := db.Where("settlement_binding_id = ? AND token_id = ? AND type = ?", binding.Id, tokenID, LogTypeConsume).First(&log).Error; err != nil {
		return nil, false, err
	}
	return &log, false, nil
}

// SettlementReadbackRelationalLogTopologyReady is intentionally strict: a
// cross-database or ClickHouse log topology cannot provide the transaction and
// foreign-key guarantees required by settlement readback.
func SettlementReadbackRelationalLogTopologyReady() bool {
	if !common.LogConsumeEnabled || DB == nil || LOG_DB == nil || DB != LOG_DB {
		return false
	}
	mainType := common.MainDatabaseType()
	if mainType != common.DatabaseTypeSQLite && mainType != common.DatabaseTypeMySQL && mainType != common.DatabaseTypePostgreSQL {
		return false
	}
	if common.LogDatabaseType() != mainType {
		return false
	}
	if mainType == common.DatabaseTypeSQLite {
		var enabled int
		if err := DB.Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil || enabled != 1 {
			return false
		}
	}
	return true
}
