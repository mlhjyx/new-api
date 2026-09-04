package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSettlementReadbackExternalOwnershipGuardMySQL(t *testing.T) {
	runSettlementReadbackExternalOwnershipGuardTest(
		t,
		common.DatabaseTypeMySQL,
		"TEST_MYSQL_DSN",
		"TEST_MYSQL_UNOWNED_DSN",
		func(dsn string) (*gorm.DB, error) {
			return gorm.Open(mysql.Open(dsn), settlementReadbackPhaseDGormConfig())
		},
	)
}

func TestSettlementReadbackExternalOwnershipGuardPostgreSQL(t *testing.T) {
	runSettlementReadbackExternalOwnershipGuardTest(
		t,
		common.DatabaseTypePostgreSQL,
		"TEST_POSTGRES_DSN",
		"TEST_POSTGRES_UNOWNED_DSN",
		func(dsn string) (*gorm.DB, error) {
			return gorm.Open(postgres.Open(dsn), settlementReadbackPhaseDGormConfig())
		},
	)
}

func runSettlementReadbackExternalOwnershipGuardTest(
	t *testing.T,
	databaseType common.DatabaseType,
	ownedDSNEnv string,
	unownedDSNEnv string,
	open func(string) (*gorm.DB, error),
) {
	t.Helper()
	ownedDSN := os.Getenv(ownedDSNEnv)
	unownedDSN := os.Getenv(unownedDSNEnv)
	if ownedDSN == "" || unownedDSN == "" {
		t.Skipf("UNPROVEN: set %s and %s to run the external ownership guard", ownedDSNEnv, unownedDSNEnv)
	}
	ownershipID := os.Getenv("TEST_SETTLEMENT_READBACK_OWNERSHIP_ID")
	ack := os.Getenv("TEST_SETTLEMENT_READBACK_DESTRUCTIVE_ACK")

	owned, err := open(ownedDSN)
	require.NoError(t, err)
	t.Cleanup(func() { closeSettlementReadbackTestDB(t, owned) })
	require.NoError(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack))
	assert.Error(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ""))
	assert.Error(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack+"-wrong"))

	require.NoError(t, owned.Exec("CREATE TABLE settlement_readback_unrelated_preexisting_object (value BIGINT)").Error)
	assert.Error(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack))
	require.NoError(t, owned.Exec("DROP TABLE settlement_readback_unrelated_preexisting_object").Error)
	require.NoError(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack))
	require.NoError(t, owned.Exec(`ALTER TABLE settlement_readback_test_ownership
		ADD CONSTRAINT settlement_readback_unrelated_check CHECK (run_id <> '')`).Error)
	assert.Error(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack),
		"complete database inventory must reject an unrelated constraint")
	if databaseType == common.DatabaseTypeMySQL {
		require.NoError(t, owned.Exec("ALTER TABLE settlement_readback_test_ownership DROP CHECK settlement_readback_unrelated_check").Error)
	} else {
		require.NoError(t, owned.Exec("ALTER TABLE settlement_readback_test_ownership DROP CONSTRAINT settlement_readback_unrelated_check").Error)
	}
	require.NoError(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack))
	if databaseType == common.DatabaseTypePostgreSQL {
		require.NoError(t, owned.Exec("CREATE SCHEMA settlement_readback_unrelated_empty_schema").Error)
		assert.Error(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack),
			"complete database inventory must reject an unrelated empty schema")
		require.NoError(t, owned.Exec("DROP SCHEMA settlement_readback_unrelated_empty_schema").Error)
		require.NoError(t, validateSettlementReadbackExternalOwnership(owned, databaseType, ownershipID, ack))
	}

	unowned, err := open(unownedDSN)
	require.NoError(t, err)
	t.Cleanup(func() { closeSettlementReadbackTestDB(t, unowned) })
	before, err := settlementReadbackExternalUserObjectInventory(unowned, databaseType)
	require.NoError(t, err)
	expectedUnownedInventory := []string{}
	if databaseType == common.DatabaseTypePostgreSQL {
		expectedUnownedInventory = []string{"schema:public"}
		assert.Equal(t, expectedUnownedInventory, before, "the negative fixture must be a pre-existing empty database")
	} else {
		assert.Empty(t, before, "the negative fixture must be a pre-existing empty database")
	}
	assert.Error(t, validateSettlementReadbackExternalOwnership(unowned, databaseType, ownershipID, ack))
	after, err := settlementReadbackExternalUserObjectInventory(unowned, databaseType)
	require.NoError(t, err)
	assert.Equal(t, before, after, "ownership rejection must perform zero writes")
	for _, table := range []string{"logs", "settlement_readback_bindings", "settlement_readback_credentials"} {
		assert.False(t, unowned.Migrator().HasTable(table))
	}
}

func closeSettlementReadbackTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err == nil {
		require.NoError(t, sqlDB.Close())
	}
}
