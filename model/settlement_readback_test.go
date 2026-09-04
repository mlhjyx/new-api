package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewSettlementReadbackCredentialStoresOnlyDerivedSecretMaterial(t *testing.T) {
	credential, secret, err := NewSettlementReadbackCredential(42, "pepper-v1", "test-pepper")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(secret), settlementReadbackSecretBytes)
	require.Equal(t, 42, credential.DispatchTokenId)
	require.Equal(t, SettlementReadbackCredentialActive, credential.Status)
	require.Equal(t, "pepper-v1", credential.PepperVersion)
	require.NotEmpty(t, credential.LookupPrefix)
	require.Len(t, credential.SecretDigest, settlementReadbackDigestHexLength)
	assert.NotContains(t, credential.LookupPrefix, secret)
	assert.NotContains(t, credential.SecretDigest, secret)
	assert.True(t, credential.MatchesSecret(secret, "test-pepper"))
	assert.False(t, credential.MatchesSecret(secret, "wrong-pepper"))
}

func TestSettlementReadbackCredentialPartsRequireExactRawBase64URLSecret(t *testing.T) {

	_, secret, err := NewSettlementReadbackCredential(42, "pepper-v1", "test-pepper")
	require.NoError(t, err)
	_, ok := settlementReadbackCredentialParts(secret)
	assert.True(t, ok)
	for _, invalid := range []string{
		"srb1.abcdefghijklmnop.short",
		"srb1.abcdefghijklmnop.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=",
		"srb1.abcdefghijklmnop.!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!",
		"srb1.abcdefghijklmnop.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		_, ok := settlementReadbackCredentialParts(invalid)
		assert.False(t, ok, invalid)
	}
}

func TestSettlementReadbackCapabilityRequiresTransactionalRelationalLogTopology(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetMainDatabaseType(originalMainType)
		common.SetLogDatabaseType(originalLogType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})

	common.LogConsumeEnabled = true
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	DB = nil
	LOG_DB = nil
	assert.False(t, SettlementReadbackRelationalLogTopologyReady())

	common.LogConsumeEnabled = false
	assert.False(t, SettlementReadbackRelationalLogTopologyReady())
}

func TestSettlementReadbackBindingAllowsSameDigestForDifferentDispatchTokens(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-composite?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettlementReadbackBinding{}))
	requestDigest := strings.Repeat("a", settlementReadbackDigestHexLength)
	nonceDigest := strings.Repeat("b", settlementReadbackDigestHexLength)
	first := &SettlementReadbackBinding{DispatchTokenId: 1, SettlementRequestIdSha256: requestDigest, SettlementNonceSha256: nonceDigest, GatewayRequestId: "gateway-1", State: SettlementReadbackBindingBound, CreatedAt: 1}
	second := &SettlementReadbackBinding{DispatchTokenId: 2, SettlementRequestIdSha256: requestDigest, SettlementNonceSha256: nonceDigest, GatewayRequestId: "gateway-2", State: SettlementReadbackBindingBound, CreatedAt: 1}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)
	duplicate := &SettlementReadbackBinding{DispatchTokenId: 1, SettlementRequestIdSha256: requestDigest, SettlementNonceSha256: strings.Repeat("c", settlementReadbackDigestHexLength), GatewayRequestId: "gateway-3", State: SettlementReadbackBindingBound, CreatedAt: 1}
	assert.Error(t, db.Create(duplicate).Error)
}

func TestSettlementReadbackBindingCasAndExactLinkedLog(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-link?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettlementReadbackBinding{}, &Log{}))
	binding := &SettlementReadbackBinding{DispatchTokenId: 9, SettlementRequestIdSha256: strings.Repeat("d", settlementReadbackDigestHexLength), SettlementNonceSha256: strings.Repeat("e", settlementReadbackDigestHexLength), GatewayRequestId: "gateway", State: SettlementReadbackBindingBound, CreatedAt: 1}
	require.NoError(t, db.Create(binding).Error)
	assert.True(t, BeginSettlementReadbackDispatch(db, binding.Id))
	assert.False(t, BeginSettlementReadbackDispatch(db, binding.Id))
	log := &Log{Type: LogTypeConsume, TokenId: 9, SettlementBindingId: &binding.Id, CreatedAt: 2}
	require.NoError(t, LinkSettlementReadbackConsumeLog(db, binding.Id, log))
	receipt, pending, err := FindSettlementReadbackConsumeLog(db, 9, binding.SettlementRequestIdSha256, binding.SettlementNonceSha256)
	require.NoError(t, err)
	assert.False(t, pending)
	require.NotNil(t, receipt)
	assert.Equal(t, binding.Id, *receipt.SettlementBindingId)
}

func TestSettlementReadbackPepperKeyringIsVersionedAndClosed(t *testing.T) {
	keyring, err := ParseSettlementReadbackPepperKeyring([]byte(strings.Join([]string{
		"schema=settlement-readback-pepper-keyring/v1",
		"pepper-v2 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"pepper-v1 VERIFY_ONLY BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"",
	}, "\n")))
	require.NoError(t, err)
	version, pepper, ok := keyring.Active()
	assert.True(t, ok)
	assert.Equal(t, "pepper-v2", version)
	assert.Len(t, pepper, settlementReadbackSecretBytes)
	_, oldPepper, ok := keyring.Lookup("pepper-v1")
	assert.True(t, ok)
	assert.Len(t, oldPepper, settlementReadbackSecretBytes)

	for name, raw := range map[string]string{
		"duplicate version": "schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\npepper-v1 VERIFY_ONLY BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB\n",
		"two active":        "schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\npepper-v2 ACTIVE BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB\n",
		"invalid secret":    "schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE padded=====================================\n",
		"unknown status":    "schema=settlement-readback-pepper-keyring/v1\npepper-v1 RETIRED AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSettlementReadbackPepperKeyring([]byte(raw))
			assert.Error(t, err)
		})
	}
}

func TestSettlementReadbackCredentialLifecycleEnforcesRotationAndRevocation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-lifecycle?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Token{}, &SettlementReadbackCredential{}, &SettlementReadbackBinding{}))
	require.NoError(t, db.Create(&Token{Id: 42, UserId: 7, Key: "lifecycle-test-token", Status: common.TokenStatusEnabled}).Error)
	keyring, err := ParseSettlementReadbackPepperKeyring([]byte(strings.Join([]string{
		"schema=settlement-readback-pepper-keyring/v1",
		"pepper-v2 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"pepper-v1 VERIFY_ONLY BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"",
	}, "\n")))
	require.NoError(t, err)
	const now int64 = 2_000_000_000

	first, firstSecret, err := CreateSettlementReadbackCredential(db, 42, keyring, now)
	require.NoError(t, err)
	assert.NotEmpty(t, firstSecret)
	assert.Equal(t, "pepper-v2", first.PepperVersion)
	_, _, err = CreateSettlementReadbackCredential(db, 42, keyring, now)
	assert.Error(t, err)

	second, secondSecret, err := RotateSettlementReadbackCredential(db, first.Id, 300, keyring, now+1)
	require.NoError(t, err)
	assert.NotEmpty(t, secondSecret)
	assert.NotEqual(t, firstSecret, secondSecret)
	require.NoError(t, db.First(first, first.Id).Error)
	assert.True(t, SettlementReadbackCredentialUsableAt(first, now+300))
	assert.False(t, SettlementReadbackCredentialUsableAt(first, now+301))
	_, _, err = RotateSettlementReadbackCredential(db, second.Id, 300, keyring, now+2)
	assert.Error(t, err, "a dispatch token may have at most two overlapping readers")

	replayed, err := RevokeSettlementReadbackCredential(db, second.Id, now+3)
	require.NoError(t, err)
	assert.False(t, replayed)
	replayed, err = RevokeSettlementReadbackCredential(db, second.Id, now+4)
	require.NoError(t, err)
	assert.True(t, replayed)
	var stored SettlementReadbackCredential
	require.NoError(t, db.First(&stored, second.Id).Error)
	assert.Equal(t, SettlementReadbackCredentialRevoked, stored.Status)
	assert.Equal(t, now+3, stored.RevokedAt)
}

func TestLoadSettlementReadbackPepperKeyringRequiresPrivateRegularFile(t *testing.T) {
	raw := []byte("schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n")
	directory := t.TempDir()
	path := filepath.Join(directory, "keyring")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	keyring, err := LoadSettlementReadbackPepperKeyring(path)
	require.NoError(t, err)
	version, _, ok := keyring.Active()
	assert.True(t, ok)
	assert.Equal(t, "pepper-v1", version)

	require.NoError(t, os.Chmod(path, 0o644))
	_, err = LoadSettlementReadbackPepperKeyring(path)
	assert.Error(t, err)
	require.NoError(t, os.Chmod(path, 0o600))
	symlink := filepath.Join(directory, "keyring-link")
	require.NoError(t, os.Symlink(path, symlink))
	_, err = LoadSettlementReadbackPepperKeyring(symlink)
	assert.Error(t, err)
}
