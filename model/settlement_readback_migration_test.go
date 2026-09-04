package model

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// legacySettlementReadbackLog is the pre-readback operational log shape. The
// migration contract is additive: existing rows and values must survive while
// settlement_binding_id is introduced as nullable.
type legacySettlementReadbackLog struct {
	Id                int   `gorm:"primaryKey"`
	UserId            int   `gorm:"index"`
	CreatedAt         int64 `gorm:"bigint"`
	Type              int
	Content           string
	Username          string
	TokenName         string
	ModelName         string
	Quota             int
	PromptTokens      int
	CompletionTokens  int
	ChannelId         int
	TokenId           int
	RequestId         string
	UpstreamRequestId string
	Other             string
}

func (legacySettlementReadbackLog) TableName() string { return "logs" }

func TestSettlementReadbackSQLiteEnablesForeignKeysOnEveryPooledConnection(t *testing.T) {
	originalPath := common.SQLitePath
	t.Cleanup(func() { common.SQLitePath = originalPath })

	common.SQLitePath = filepath.Join(t.TempDir(), "settlement-readback.db") + "?_busy_timeout=30000"
	t.Setenv("SQL_DSN", "local")
	db, databaseType, err := chooseDB("SQL_DSN", false)
	require.NoError(t, err)
	require.Equal(t, common.DatabaseTypeSQLite, databaseType)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(4)

	connections := make([]*sql.Conn, 0, 4)
	for index := 0; index < 4; index++ {
		connection, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		connections = append(connections, connection)
	}
	t.Cleanup(func() {
		for _, connection := range connections {
			require.NoError(t, connection.Close())
		}
	})

	for index, connection := range connections {
		var enabled int
		require.NoError(t, connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled))
		assert.Equalf(t, 1, enabled, "pooled sqlite connection %d must enforce foreign keys", index)
	}
}

func TestSettlementReadbackMigrationPreservesPopulatedLegacyLogs(t *testing.T) {
	db, err := openSettlementReadbackSQLite("legacy-forward-upgrade")
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&legacySettlementReadbackLog{}))
	legacyRows := []legacySettlementReadbackLog{
		{
			Id: 1, UserId: 10, CreatedAt: 1_700_000_001, Type: LogTypeConsume,
			Content: "legacy content one", Username: "legacy-user", TokenName: "legacy-token",
			ModelName: "legacy-model", Quota: 42, PromptTokens: 7, CompletionTokens: 9,
			ChannelId: 3, TokenId: 11, RequestId: "legacy-request-1",
			UpstreamRequestId: "legacy-upstream-1", Other: `{"legacy":true}`,
		},
		{
			Id: 2, UserId: 20, CreatedAt: 1_700_000_002, Type: LogTypeError,
			Content: "legacy content two", Username: "second-user", TokenName: "second-token",
			ModelName: "second-model", Quota: math.MaxInt32, PromptTokens: 100, CompletionTokens: 200,
			ChannelId: 4, TokenId: 12, RequestId: "legacy-request-2",
			UpstreamRequestId: "", Other: `{"legacy":"preserve"}`,
		},
	}
	require.NoError(t, db.Create(&legacyRows).Error)

	require.NoError(t, EnsureSettlementReadbackSharedSchema(db))

	var migrated []legacySettlementReadbackLog
	require.NoError(t, db.Order("id ASC").Find(&migrated).Error)
	assert.Equal(t, legacyRows, migrated)

	for _, row := range legacyRows {
		var settlementBindingID *int
		require.NoError(t, db.Raw("SELECT settlement_binding_id FROM logs WHERE id = ?", row.Id).Scan(&settlementBindingID).Error)
		assert.Nil(t, settlementBindingID)
	}
}

func TestSettlementReadbackReadinessRejectsNonUniqueContractIndexes(t *testing.T) {
	tests := []struct {
		name        string
		dropModel   any
		indexName   string
		replacement string
	}{
		{
			name:        "reader lookup prefix",
			dropModel:   &SettlementReadbackCredential{},
			indexName:   "idx_settlement_readback_credentials_lookup_prefix",
			replacement: "CREATE INDEX idx_settlement_readback_credentials_lookup_prefix ON settlement_readback_credentials(lookup_prefix)",
		},
		{
			name:        "token request digest",
			dropModel:   &SettlementReadbackBinding{},
			indexName:   "idx_settlement_request",
			replacement: "CREATE INDEX idx_settlement_request ON settlement_readback_bindings(dispatch_token_id, settlement_request_id_sha256)",
		},
		{
			name:        "token nonce digest",
			dropModel:   &SettlementReadbackBinding{},
			indexName:   "idx_settlement_nonce",
			replacement: "CREATE INDEX idx_settlement_nonce ON settlement_readback_bindings(dispatch_token_id, settlement_nonce_sha256)",
		},
		{
			name:        "linked log",
			dropModel:   &Log{},
			indexName:   "idx_logs_settlement_binding_id",
			replacement: "CREATE INDEX idx_logs_settlement_binding_id ON logs(settlement_binding_id)",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := openSettlementReadbackSQLite("nonunique-" + testCase.indexName)
			require.NoError(t, err)
			require.NoError(t, EnsureSettlementReadbackSharedSchema(db))
			restoreSettlementReadbackTestTopology(t, db, common.DatabaseTypeSQLite)
			assert.True(t, SettlementReadbackRelationalLogTopologyReady())

			require.NoError(t, db.Migrator().DropIndex(testCase.dropModel, testCase.indexName))
			require.NoError(t, db.Exec(testCase.replacement).Error)
			assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "a same-name non-unique index must not satisfy the contract")
		})
	}
}

func restoreSettlementReadbackTestTopology(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	originalDB, originalLogDB := DB, LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(databaseType, databaseType)
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
}

func openSettlementReadbackSQLite(name string) (*gorm.DB, error) {
	path := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", name)
	return gorm.Open(sqlite.Open(path), &gorm.Config{})
}
