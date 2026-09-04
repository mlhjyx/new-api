package model

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func settlementReadbackPhaseDGormConfig() *gorm.Config {
	return &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
}

func TestSettlementReadbackPhaseDSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:settlement-phase-d?mode=memory&cache=shared&_pragma=foreign_keys(1)&_pragma=busy_timeout(30000)"), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	runSettlementReadbackPhaseDContract(t, db, common.DatabaseTypeSQLite)
}

func TestSettlementReadbackPhaseDMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_MYSQL_DSN to run the MySQL settlement readback Phase D contract")
	}
	db, err := gorm.Open(mysql.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	runSettlementReadbackExternalPhaseDContract(t, db, common.DatabaseTypeMySQL)
}

func TestSettlementReadbackPhaseDPostgreSQL(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_POSTGRES_DSN to run the PostgreSQL settlement readback Phase D contract")
	}
	db, err := gorm.Open(postgres.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	runSettlementReadbackExternalPhaseDContract(t, db, common.DatabaseTypePostgreSQL)
}

func TestSettlementReadbackReadinessRejectsMySQLForeignKeyActionDrift(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_MYSQL_DSN to run the MySQL foreign-key drift gate")
	}
	db, err := gorm.Open(mysql.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	runSettlementReadbackExternalForeignKeyDrift(t, db, common.DatabaseTypeMySQL)
}

func TestSettlementReadbackReadinessRejectsPostgreSQLForeignKeyActionDrift(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_POSTGRES_DSN to run the PostgreSQL foreign-key drift gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	runSettlementReadbackExternalForeignKeyDrift(t, db, common.DatabaseTypePostgreSQL)
}

func TestSettlementReadbackReadinessRejectsPostgreSQLForeignKeySchemaDrift(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_POSTGRES_DSN to run the PostgreSQL foreign-key schema drift gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	requireSettlementReadbackExternalOwnership(t, db, common.DatabaseTypePostgreSQL)
	for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("refusing foreign-key schema drift test against non-empty PostgreSQL database: table %s already exists", table)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
			if db.Migrator().HasTable(table) {
				require.NoError(t, db.Migrator().DropTable(table))
			}
		}
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS settlement_phase_d_foreign.settlement_readback_bindings").Error)
		require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS settlement_phase_d_foreign").Error)
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, EnsureSettlementReadbackSharedSchema(db))
	restoreSettlementReadbackTestTopology(t, db, common.DatabaseTypePostgreSQL)
	assert.True(t, SettlementReadbackRelationalLogTopologyReady())
	require.NoError(t, db.Exec("CREATE SCHEMA settlement_phase_d_foreign").Error)
	require.NoError(t, db.Exec("CREATE TABLE settlement_phase_d_foreign.settlement_readback_bindings (id BIGINT PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("ALTER TABLE logs DROP CONSTRAINT fk_logs_settlement_binding").Error)
	require.NoError(t, db.Exec(`ALTER TABLE logs ADD CONSTRAINT fk_logs_settlement_binding
		FOREIGN KEY (settlement_binding_id)
		REFERENCES settlement_phase_d_foreign.settlement_readback_bindings(id)
		ON UPDATE RESTRICT ON DELETE RESTRICT`).Error)

	assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "a cross-schema relation must not satisfy the shared-database contract")
}

func TestSettlementReadbackReadinessRejectsMySQLPrefixUniqueIndex(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_MYSQL_DSN to run the MySQL prefix-index drift gate")
	}
	db, err := gorm.Open(mysql.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	prepareSettlementReadbackExternalDriftDB(t, db, common.DatabaseTypeMySQL)

	require.NoError(t, db.Migrator().DropIndex(&SettlementReadbackBinding{}, "idx_settlement_request"))
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_settlement_request
		ON settlement_readback_bindings(dispatch_token_id, settlement_request_id_sha256(8))`).Error)
	assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "a prefix-only digest index must not satisfy the exact uniqueness contract")
}

func TestSettlementReadbackReadinessRejectsMySQLReversedIndexOrder(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_MYSQL_DSN to run the MySQL index-order drift gate")
	}
	db, err := gorm.Open(mysql.Open(dsn), settlementReadbackPhaseDGormConfig())
	require.NoError(t, err)
	prepareSettlementReadbackExternalDriftDB(t, db, common.DatabaseTypeMySQL)

	require.NoError(t, db.Migrator().DropIndex(&SettlementReadbackBinding{}, "idx_settlement_request"))
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_settlement_request
		ON settlement_readback_bindings(settlement_request_id_sha256, dispatch_token_id)`).Error)
	assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "reversed key order must not satisfy the exact uniqueness contract")
}

func TestSettlementReadbackReadinessRejectsPostgreSQLIndexCatalogDrift(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("UNPROVEN: set TEST_POSTGRES_DSN to run the PostgreSQL index-catalog drift gates")
	}
	tests := []struct {
		name   string
		mutate func(*testing.T, *gorm.DB)
	}{
		{
			name: "other schema same name",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Exec("DROP INDEX idx_settlement_request").Error)
				require.NoError(t, db.Exec("CREATE SCHEMA settlement_phase_d_index_shadow").Error)
				t.Cleanup(func() {
					require.NoError(t, db.Exec("DROP SCHEMA IF EXISTS settlement_phase_d_index_shadow CASCADE").Error)
				})
				require.NoError(t, db.Exec(`CREATE TABLE settlement_phase_d_index_shadow.settlement_readback_bindings (
					dispatch_token_id BIGINT NOT NULL,
					settlement_request_id_sha256 CHAR(64) NOT NULL
				)`).Error)
				require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_settlement_request
					ON settlement_phase_d_index_shadow.settlement_readback_bindings(dispatch_token_id, settlement_request_id_sha256)`).Error)
			},
		},
		{
			name: "partial",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Exec("DROP INDEX idx_settlement_request").Error)
				require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_settlement_request
					ON settlement_readback_bindings(dispatch_token_id, settlement_request_id_sha256)
					WHERE dispatch_token_id > 0`).Error)
			},
		},
		{
			name: "reversed key attnums",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Exec("DROP INDEX idx_settlement_request").Error)
				require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_settlement_request
					ON settlement_readback_bindings(settlement_request_id_sha256, dispatch_token_id)`).Error)
			},
		},
		{
			name: "invalid",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Exec(`UPDATE pg_index SET indisvalid = false
					WHERE indexrelid = 'idx_settlement_request'::regclass`).Error)
			},
		},
		{
			name: "not ready",
			mutate: func(t *testing.T, db *gorm.DB) {
				require.NoError(t, db.Exec(`UPDATE pg_index SET indisready = false
					WHERE indexrelid = 'idx_settlement_request'::regclass`).Error)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := gorm.Open(postgres.Open(dsn), settlementReadbackPhaseDGormConfig())
			require.NoError(t, err)
			prepareSettlementReadbackExternalDriftDB(t, db, common.DatabaseTypePostgreSQL)
			testCase.mutate(t, db)
			assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "catalog drift must fail closed")
		})
	}
}

func prepareSettlementReadbackExternalDriftDB(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	requireSettlementReadbackExternalOwnership(t, db, databaseType)
	for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("refusing index drift test against non-empty %s database: table %s already exists", databaseType, table)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
			if db.Migrator().HasTable(table) {
				require.NoError(t, db.Migrator().DropTable(table))
			}
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, EnsureSettlementReadbackSharedSchema(db))
	restoreSettlementReadbackTestTopology(t, db, databaseType)
	assert.True(t, SettlementReadbackRelationalLogTopologyReady())
}

func runSettlementReadbackExternalForeignKeyDrift(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	requireSettlementReadbackExternalOwnership(t, db, databaseType)
	for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("refusing foreign-key drift test against non-empty %s database: table %s already exists", databaseType, table)
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
			if db.Migrator().HasTable(table) {
				require.NoError(t, db.Migrator().DropTable(table))
			}
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, EnsureSettlementReadbackSharedSchema(db))
	restoreSettlementReadbackTestTopology(t, db, databaseType)
	assert.True(t, SettlementReadbackRelationalLogTopologyReady())

	switch databaseType {
	case common.DatabaseTypeMySQL:
		require.NoError(t, db.Exec("ALTER TABLE logs DROP FOREIGN KEY fk_logs_settlement_binding").Error)
		require.NoError(t, db.Exec(`ALTER TABLE logs ADD CONSTRAINT fk_logs_settlement_binding
			FOREIGN KEY (settlement_binding_id) REFERENCES settlement_readback_bindings(id)
			ON UPDATE CASCADE ON DELETE CASCADE`).Error)
	case common.DatabaseTypePostgreSQL:
		require.NoError(t, db.Exec("ALTER TABLE logs DROP CONSTRAINT fk_logs_settlement_binding").Error)
		require.NoError(t, db.Exec(`ALTER TABLE logs ADD CONSTRAINT fk_logs_settlement_binding
			FOREIGN KEY (settlement_binding_id) REFERENCES settlement_readback_bindings(id)
			ON UPDATE CASCADE ON DELETE CASCADE`).Error)
	default:
		t.Fatalf("unsupported database type %q", databaseType)
	}
	assert.False(t, SettlementReadbackRelationalLogTopologyReady(), "cascade actions must not satisfy the retention contract")
}

func runSettlementReadbackExternalPhaseDContract(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	requireSettlementReadbackExternalOwnership(t, db, databaseType)
	for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("refusing Phase D test against non-empty %s database: table %s already exists", databaseType, table)
		}
	}

	t.Cleanup(func() {
		dropSettlementReadbackPhaseDTrigger(t, db, databaseType)
		for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
			if db.Migrator().HasTable(table) {
				require.NoError(t, db.Migrator().DropTable(table))
			}
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	runSettlementReadbackPhaseDContract(t, db, databaseType)
}

func runSettlementReadbackPhaseDContract(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(24)
	sqlDB.SetMaxIdleConns(24)

	assertSettlementReadbackLegacyMigration(t, db, databaseType)
	assert.True(t, SettlementReadbackRelationalLogTopologyReady())

	assertSettlementReadbackPhaseDSchema(t, db, databaseType)
	assertSettlementReadbackPhaseDUniqueness(t, db)
	assertSettlementReadbackPhaseDCAS(t, db)
	assertSettlementReadbackPhaseDRollback(t, db, databaseType)
	assertSettlementReadbackPhaseDInt64Receipt(t, db)
}

func assertSettlementReadbackPhaseDSchema(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	for _, table := range []string{"settlement_readback_credentials", "settlement_readback_bindings", "logs"} {
		assert.Truef(t, db.Migrator().HasTable(table), "missing table %s", table)
	}

	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "lookup_prefix", "varchar", 32, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "secret_digest", "char", 64, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "pepper_version", "varchar", 64, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "dispatch_token_id", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "status", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "created_at", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "revoked_at", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackCredential{}, databaseType, "rotation_ends_at", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "dispatch_token_id", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "settlement_request_id_sha256", "char", 64, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "settlement_nonce_sha256", "char", 64, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "gateway_request_id", "varchar", 64, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "state", "varchar", 32, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "created_at", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &SettlementReadbackBinding{}, databaseType, "log_linked_at", "signed64", 0, false)
	assertSettlementReadbackColumn(t, db, &Log{}, databaseType, "settlement_binding_id", "signed64", 0, true)

	assert.True(t, settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackCredential{}, "idx_settlement_readback_credentials_lookup_prefix", []string{"lookup_prefix"}))
	assert.True(t, settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackBinding{}, "idx_settlement_request", []string{"dispatch_token_id", "settlement_request_id_sha256"}))
	assert.True(t, settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackBinding{}, "idx_settlement_nonce", []string{"dispatch_token_id", "settlement_nonce_sha256"}))
	assert.True(t, settlementReadbackHasExactUniqueIndex(db, &Log{}, "idx_logs_settlement_binding_id", []string{"settlement_binding_id"}))
	assert.True(t, db.Migrator().HasConstraint(&settlementReadbackLinkedLog{}, "SettlementBinding"))

	orphanBindingID := 9_999_999
	orphan := &Log{Type: LogTypeConsume, TokenId: 1, SettlementBindingId: &orphanBindingID, CreatedAt: 1}
	assert.Error(t, db.Create(orphan).Error, "log foreign key must reject an unknown binding")
}

func assertSettlementReadbackColumn(t *testing.T, db *gorm.DB, table any, databaseType common.DatabaseType, name string, semanticType string, length int64, nullable bool) {
	t.Helper()
	columnTypes, err := db.Migrator().ColumnTypes(table)
	require.NoError(t, err)
	for _, columnType := range columnTypes {
		if strings.EqualFold(columnType.Name(), name) {
			actualType := strings.ToLower(columnType.DatabaseTypeName())
			switch semanticType {
			case "signed64":
				allowed := map[common.DatabaseType][]string{
					common.DatabaseTypeSQLite:     {"integer", "bigint"},
					common.DatabaseTypeMySQL:      {"bigint"},
					common.DatabaseTypePostgreSQL: {"int8", "bigint"},
				}
				assert.Contains(t, allowed[databaseType], actualType, "column %s must preserve signed 64-bit integers", name)
			case "char", "varchar":
				allowed := map[string][]string{
					"char":    {"char", "bpchar", "character"},
					"varchar": {"varchar", "character varying"},
				}
				assert.Contains(t, allowed[semanticType], actualType, "column %s has wrong string type", name)
				actualLength, known := columnType.Length()
				require.Truef(t, known, "column %s length must be introspectable", name)
				assert.Equal(t, length, actualLength, "column %s has wrong length", name)
			default:
				t.Fatalf("unknown semantic type %q", semanticType)
			}
			actualNullable, known := phaseDColumnNullable(db, table, databaseType, name, columnType)
			require.Truef(t, known, "column %s nullability must be introspectable", name)
			assert.Equal(t, nullable, actualNullable, "column %s has wrong nullability", name)
			return
		}
	}
	t.Fatalf("column %s not found", name)
}

func phaseDColumnNullable(db *gorm.DB, table any, databaseType common.DatabaseType, name string, columnType gorm.ColumnType) (bool, bool) {
	if databaseType != common.DatabaseTypeSQLite {
		return columnType.Nullable()
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(table); err != nil || statement.Schema == nil {
		return false, false
	}
	var rows []struct {
		NotNull int `gorm:"column:not_null"`
	}
	err := db.Raw(
		`SELECT "notnull" AS not_null FROM pragma_table_info(?) WHERE name = ?`,
		statement.Schema.Table,
		name,
	).Scan(&rows).Error
	if err != nil || len(rows) != 1 {
		return false, false
	}
	return rows[0].NotNull == 0, true
}

func assertSettlementReadbackPhaseDUniqueness(t *testing.T, db *gorm.DB) {
	t.Helper()
	requestDigest := strings.Repeat("a", 64)
	nonceDigest := strings.Repeat("b", 64)
	first := newPhaseDBinding(101, requestDigest, nonceDigest, "gateway-101")
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(newPhaseDBinding(102, requestDigest, nonceDigest, "gateway-102")).Error, "composite uniqueness must be scoped by dispatch token")
	assert.Error(t, db.Create(newPhaseDBinding(101, requestDigest, strings.Repeat("c", 64), "gateway-103")).Error)
	assert.Error(t, db.Create(newPhaseDBinding(101, strings.Repeat("d", 64), nonceDigest, "gateway-104")).Error)

	firstNull := &Log{Type: LogTypeError, TokenId: 101, CreatedAt: 2}
	secondNull := &Log{Type: LogTypeError, TokenId: 101, CreatedAt: 3}
	require.NoError(t, db.Create(firstNull).Error)
	require.NoError(t, db.Create(secondNull).Error, "nullable unique relation must allow multiple unrelated legacy logs")
}

func assertSettlementReadbackPhaseDCAS(t *testing.T, db *gorm.DB) {
	t.Helper()
	binding := newPhaseDBinding(201, strings.Repeat("e", 64), strings.Repeat("f", 64), "gateway-201")
	require.NoError(t, db.Create(binding).Error)

	var winners atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for attempt := 0; attempt < 16; attempt++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if BeginSettlementReadbackDispatch(db, binding.Id) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	assert.EqualValues(t, 1, winners.Load(), "BOUND -> DISPATCH_STARTED must have exactly one winner")
	var stored SettlementReadbackBinding
	require.NoError(t, db.First(&stored, binding.Id).Error)
	assert.Equal(t, SettlementReadbackBindingDispatchStarted, stored.State)
}

func assertSettlementReadbackPhaseDRollback(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	binding := newPhaseDBinding(301, strings.Repeat("1", 64), strings.Repeat("2", 64), "gateway-301")
	require.NoError(t, db.Create(binding).Error)
	require.True(t, BeginSettlementReadbackDispatch(db, binding.Id))
	require.NoError(t, installSettlementReadbackPhaseDTrigger(db, databaseType))
	t.Cleanup(func() { dropSettlementReadbackPhaseDTrigger(t, db, databaseType) })

	log := &Log{Type: LogTypeConsume, TokenId: binding.DispatchTokenId, SettlementBindingId: &binding.Id, CreatedAt: 4}
	err := LinkSettlementReadbackConsumeLog(db, binding.Id, log)
	require.Error(t, err)
	assert.True(t, IsSettlementReadbackIntegrityError(err))
	var count int64
	require.NoError(t, db.Model(&Log{}).Where("settlement_binding_id = ?", binding.Id).Count(&count).Error)
	assert.Zero(t, count, "log insert must roll back when final LOG_LINKED CAS loses")
	var stored SettlementReadbackBinding
	require.NoError(t, db.First(&stored, binding.Id).Error)
	assert.Equal(t, SettlementReadbackBindingDispatchStarted, stored.State, "trigger state change must roll back with the failed transaction")

	dropSettlementReadbackPhaseDTrigger(t, db, databaseType)
}

func assertSettlementReadbackPhaseDInt64Receipt(t *testing.T, db *gorm.DB) {
	t.Helper()
	binding := newPhaseDBinding(401, strings.Repeat("3", 64), strings.Repeat("4", 64), "gateway-401")
	require.NoError(t, db.Create(binding).Error)
	require.True(t, BeginSettlementReadbackDispatch(db, binding.Id))
	log := &Log{
		Type: LogTypeConsume, TokenId: binding.DispatchTokenId, SettlementBindingId: &binding.Id,
		CreatedAt: 5, ModelName: "model-int64", ChannelId: 1,
		Quota: math.MaxInt64, PromptTokens: math.MaxInt64, CompletionTokens: 0,
		Other: `{"usage_semantic":"openai","cache_creation_tokens":0,"cache_tokens":9223372036854775807}`,
	}
	require.NoError(t, LinkSettlementReadbackConsumeLog(db, binding.Id, log))
	receipt, pending, err := FindSettlementReadbackConsumeLog(db, binding.DispatchTokenId, binding.SettlementRequestIdSha256, binding.SettlementNonceSha256)
	require.NoError(t, err)
	assert.False(t, pending)
	require.NotNil(t, receipt)
	assert.Equal(t, int(math.MaxInt64), receipt.Quota)
	assert.Equal(t, int(math.MaxInt64), receipt.PromptTokens)
	assert.Zero(t, receipt.CompletionTokens)
}

func newPhaseDBinding(tokenID int, requestDigest string, nonceDigest string, gatewayRequestID string) *SettlementReadbackBinding {
	return &SettlementReadbackBinding{
		DispatchTokenId: tokenID, SettlementRequestIdSha256: requestDigest,
		SettlementNonceSha256: nonceDigest, GatewayRequestId: gatewayRequestID,
		State: SettlementReadbackBindingBound, CreatedAt: 1,
	}
}

func installSettlementReadbackPhaseDTrigger(db *gorm.DB, databaseType common.DatabaseType) error {
	switch databaseType {
	case common.DatabaseTypeSQLite:
		return db.Exec(`CREATE TRIGGER settlement_phase_d_state_drift
			AFTER INSERT ON logs WHEN NEW.settlement_binding_id IS NOT NULL
			BEGIN
				UPDATE settlement_readback_bindings SET state = 'BOUND' WHERE id = NEW.settlement_binding_id;
			END`).Error
	case common.DatabaseTypeMySQL:
		return db.Exec(`CREATE TRIGGER settlement_phase_d_state_drift
			AFTER INSERT ON logs FOR EACH ROW
			UPDATE settlement_readback_bindings SET state = 'BOUND' WHERE id = NEW.settlement_binding_id`).Error
	case common.DatabaseTypePostgreSQL:
		if err := db.Exec(`CREATE OR REPLACE FUNCTION settlement_phase_d_state_drift_fn() RETURNS trigger AS $$
			BEGIN
				UPDATE settlement_readback_bindings SET state = 'BOUND' WHERE id = NEW.settlement_binding_id;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql`).Error; err != nil {
			return err
		}
		return db.Exec(`CREATE TRIGGER settlement_phase_d_state_drift
			AFTER INSERT ON logs FOR EACH ROW EXECUTE FUNCTION settlement_phase_d_state_drift_fn()`).Error
	default:
		return fmt.Errorf("unsupported Phase D database type %q", databaseType)
	}
}

func dropSettlementReadbackPhaseDTrigger(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	if !db.Migrator().HasTable("logs") {
		return
	}
	switch databaseType {
	case common.DatabaseTypeSQLite:
		require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS settlement_phase_d_state_drift").Error)
	case common.DatabaseTypeMySQL:
		require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS settlement_phase_d_state_drift").Error)
	case common.DatabaseTypePostgreSQL:
		require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS settlement_phase_d_state_drift ON logs").Error)
		require.NoError(t, db.Exec("DROP FUNCTION IF EXISTS settlement_phase_d_state_drift_fn()").Error)
	}
}
