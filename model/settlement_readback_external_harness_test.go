package model

import (
	"fmt"
	"os"
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const settlementReadbackDestructiveAckPrefix = "settlement-readback-phase-d-destructive/v1:"
const settlementReadbackOwnershipTable = "settlement_readback_test_ownership"

var settlementReadbackOwnershipIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type settlementReadbackOwnershipSentinel struct {
	RunID        string `gorm:"column:run_id"`
	Dialect      string `gorm:"column:dialect"`
	DatabaseName string `gorm:"column:database_name"`
}

func requireSettlementReadbackExternalOwnership(t require.TestingT, db *gorm.DB, databaseType common.DatabaseType) {
	err := validateSettlementReadbackExternalOwnership(
		db,
		databaseType,
		os.Getenv("TEST_SETTLEMENT_READBACK_OWNERSHIP_ID"),
		os.Getenv("TEST_SETTLEMENT_READBACK_DESTRUCTIVE_ACK"),
	)
	require.NoError(t, err)
}

func validateSettlementReadbackExternalOwnership(db *gorm.DB, databaseType common.DatabaseType, ownershipID string, destructiveAck string) error {
	if db == nil || !settlementReadbackOwnershipIDPattern.MatchString(ownershipID) {
		return fmt.Errorf("settlement readback external database ownership is invalid")
	}
	if destructiveAck != settlementReadbackDestructiveAckPrefix+ownershipID {
		return fmt.Errorf("settlement readback destructive acknowledgement is invalid")
	}
	expectedDatabase := "settlement_phase_d_" + ownershipID
	currentDatabase, err := settlementReadbackExternalCurrentDatabase(db, databaseType)
	if err != nil || currentDatabase != expectedDatabase {
		return fmt.Errorf("settlement readback external database identity is invalid")
	}
	inventory, err := settlementReadbackExternalUserObjectInventory(db, databaseType)
	expectedInventory := []string{"relation:" + settlementReadbackOwnershipTable}
	if databaseType == common.DatabaseTypePostgreSQL {
		expectedInventory = []string{"relation:public:" + settlementReadbackOwnershipTable, "schema:public"}
	}
	if err != nil || !settlementReadbackInventoryEqual(inventory, expectedInventory) {
		return fmt.Errorf("settlement readback external database inventory is not exclusively owned")
	}
	var sentinels []settlementReadbackOwnershipSentinel
	if err := db.Raw(`SELECT run_id, dialect, database_name
		FROM settlement_readback_test_ownership LIMIT 2`).Scan(&sentinels).Error; err != nil || len(sentinels) != 1 {
		return fmt.Errorf("settlement readback ownership sentinel is invalid")
	}
	expectedDialect := string(databaseType)
	if databaseType == common.DatabaseTypePostgreSQL {
		expectedDialect = "postgres"
	}
	sentinel := sentinels[0]
	if sentinel.RunID != ownershipID || sentinel.Dialect != expectedDialect || sentinel.DatabaseName != expectedDatabase {
		return fmt.Errorf("settlement readback ownership sentinel does not match the database")
	}
	return nil
}

func settlementReadbackInventoryEqual(actual []string, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func settlementReadbackExternalCurrentDatabase(db *gorm.DB, databaseType common.DatabaseType) (string, error) {
	var name string
	switch databaseType {
	case common.DatabaseTypeMySQL:
		return name, db.Raw("SELECT DATABASE()").Scan(&name).Error
	case common.DatabaseTypePostgreSQL:
		return name, db.Raw("SELECT current_database()").Scan(&name).Error
	default:
		return "", fmt.Errorf("unsupported settlement readback external database")
	}
}

func settlementReadbackExternalUserObjectInventory(db *gorm.DB, databaseType common.DatabaseType) ([]string, error) {
	var inventory []string
	var query string
	switch databaseType {
	case common.DatabaseTypeMySQL:
		query = `SELECT object_name FROM (
			SELECT CONCAT('relation:', TABLE_NAME) AS object_name
			  FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE()
			UNION ALL
			SELECT CONCAT('index:', TABLE_NAME, ':', INDEX_NAME) AS object_name
			  FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE()
			UNION ALL
			SELECT CONCAT('trigger:', TRIGGER_NAME) AS object_name
			  FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE()
			UNION ALL
			SELECT CONCAT('routine:', ROUTINE_TYPE, ':', ROUTINE_NAME) AS object_name
			  FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA = DATABASE()
			UNION ALL
			SELECT CONCAT('event:', EVENT_NAME) AS object_name
			  FROM information_schema.EVENTS WHERE EVENT_SCHEMA = DATABASE()
			UNION ALL
			SELECT CONCAT('constraint:', TABLE_NAME, ':', CONSTRAINT_NAME, ':', CONSTRAINT_TYPE) AS object_name
			  FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = DATABASE()
		) owned_objects ORDER BY object_name`
	case common.DatabaseTypePostgreSQL:
		query = `SELECT object_name FROM (
			SELECT CASE c.relkind
				WHEN 'i' THEN 'index:' || n.nspname || ':' || c.relname
				WHEN 'I' THEN 'index:' || n.nspname || ':' || c.relname
				ELSE 'relation:' || n.nspname || ':' || c.relname
			END AS object_name
			  FROM pg_catalog.pg_class c
			  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
			   AND c.relkind IN ('r','p','v','m','S','f','i','I')
			UNION ALL
			SELECT 'routine:' || n.nspname || ':' || p.proname
			  FROM pg_catalog.pg_proc p
			  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
			UNION ALL
			SELECT 'trigger:' || n.nspname || ':' || c.relname || ':' || t.tgname
			  FROM pg_catalog.pg_trigger t
			  JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
			  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema' AND NOT t.tgisinternal
			UNION ALL
			SELECT 'policy:' || n.nspname || ':' || c.relname || ':' || p.polname
			  FROM pg_catalog.pg_policy p
			  JOIN pg_catalog.pg_class c ON c.oid = p.polrelid
			  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
			UNION ALL
			SELECT 'type:' || n.nspname || ':' || t.typname
			  FROM pg_catalog.pg_type t
			  JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
			   AND t.typtype IN ('d','e','r','m')
			UNION ALL
			SELECT 'constraint:' || n.nspname || ':' || COALESCE(c.relname, '') || ':' || con.conname
			  FROM pg_catalog.pg_constraint con
			  JOIN pg_catalog.pg_namespace n ON n.oid = con.connamespace
			  LEFT JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
			UNION ALL
			SELECT 'schema:' || n.nspname
			  FROM pg_catalog.pg_namespace n
			 WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema'
		) owned_objects ORDER BY object_name`
	default:
		return nil, fmt.Errorf("unsupported settlement readback external database")
	}
	if err := db.Raw(query).Scan(&inventory).Error; err != nil {
		return nil, err
	}
	return inventory, nil
}
