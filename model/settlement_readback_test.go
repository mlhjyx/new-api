package model

import (
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
