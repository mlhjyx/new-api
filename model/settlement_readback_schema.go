package model

import (
	"strings"

	"gorm.io/gorm"
)

func settlementReadbackSchemaReady(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	migrator := db.Migrator()
	return migrator.HasTable(&SettlementReadbackCredential{}) &&
		migrator.HasTable(&SettlementReadbackBinding{}) &&
		migrator.HasTable(&Log{}) &&
		migrator.HasColumn(&Log{}, "SettlementBindingId") &&
		settlementReadbackHasExactColumns(db, &SettlementReadbackCredential{}, []settlementReadbackColumnContract{
			{name: "lookup_prefix", semanticType: "varchar", length: 32},
			{name: "secret_digest", semanticType: "char", length: 64},
			{name: "pepper_version", semanticType: "varchar", length: 64},
			{name: "dispatch_token_id", semanticType: "signed64"},
			{name: "status", semanticType: "signed64"},
			{name: "created_at", semanticType: "signed64"},
			{name: "revoked_at", semanticType: "signed64"},
			{name: "rotation_ends_at", semanticType: "signed64"},
		}) &&
		settlementReadbackHasExactColumns(db, &SettlementReadbackBinding{}, []settlementReadbackColumnContract{
			{name: "dispatch_token_id", semanticType: "signed64"},
			{name: "settlement_request_id_sha256", semanticType: "char", length: 64},
			{name: "settlement_nonce_sha256", semanticType: "char", length: 64},
			{name: "gateway_request_id", semanticType: "varchar", length: 64},
			{name: "state", semanticType: "varchar", length: 32},
			{name: "created_at", semanticType: "signed64"},
			{name: "log_linked_at", semanticType: "signed64"},
		}) &&
		settlementReadbackHasExactColumns(db, &Log{}, []settlementReadbackColumnContract{
			{name: "settlement_binding_id", semanticType: "signed64", nullable: true},
		}) &&
		settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackCredential{}, "idx_settlement_readback_credentials_lookup_prefix", []string{"lookup_prefix"}) &&
		settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackBinding{}, "idx_settlement_request", []string{"dispatch_token_id", "settlement_request_id_sha256"}) &&
		settlementReadbackHasExactUniqueIndex(db, &SettlementReadbackBinding{}, "idx_settlement_nonce", []string{"dispatch_token_id", "settlement_nonce_sha256"}) &&
		settlementReadbackHasExactUniqueIndex(db, &Log{}, "idx_logs_settlement_binding_id", []string{"settlement_binding_id"}) &&
		settlementReadbackHasExactForeignKey(db)
}

type settlementReadbackColumnContract struct {
	name         string
	semanticType string
	length       int64
	nullable     bool
}

func settlementReadbackHasExactColumns(db *gorm.DB, table any, contracts []settlementReadbackColumnContract) bool {
	if db == nil || len(contracts) == 0 {
		return false
	}
	columnTypes, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return false
	}
	columns := make(map[string]gorm.ColumnType, len(columnTypes))
	for _, columnType := range columnTypes {
		columns[strings.ToLower(columnType.Name())] = columnType
	}
	for _, contract := range contracts {
		columnType, ok := columns[contract.name]
		if !ok || !settlementReadbackColumnTypeMatches(db.Dialector.Name(), columnType, contract) {
			return false
		}
		nullable, known := settlementReadbackColumnNullable(db, table, columnType)
		if !known || nullable != contract.nullable {
			return false
		}
	}
	return true
}

func settlementReadbackColumnTypeMatches(dialect string, columnType gorm.ColumnType, contract settlementReadbackColumnContract) bool {
	actual := strings.ToLower(columnType.DatabaseTypeName())
	switch contract.semanticType {
	case "signed64":
		switch dialect {
		case "sqlite":
			return actual == "integer" || actual == "bigint"
		case "mysql":
			return actual == "bigint"
		case "postgres":
			return actual == "int8" || actual == "bigint"
		default:
			return false
		}
	case "char":
		if actual != "char" && actual != "bpchar" && actual != "character" {
			return false
		}
	case "varchar":
		if actual != "varchar" && actual != "character varying" {
			return false
		}
	default:
		return false
	}
	length, known := columnType.Length()
	return known && length == contract.length
}

func settlementReadbackColumnNullable(db *gorm.DB, table any, columnType gorm.ColumnType) (bool, bool) {
	if db.Dialector.Name() != "sqlite" {
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
		columnType.Name(),
	).Scan(&rows).Error
	if err != nil || len(rows) != 1 {
		return false, false
	}
	return rows[0].NotNull == 0, true
}

type settlementReadbackForeignKeyContract struct {
	ReferencedTable  string `gorm:"column:referenced_table"`
	SourceColumn     string `gorm:"column:source_column"`
	ReferencedColumn string `gorm:"column:referenced_column"`
	UpdateRule       string `gorm:"column:update_rule"`
	DeleteRule       string `gorm:"column:delete_rule"`
	SameSchema       int    `gorm:"column:same_schema"`
}

func settlementReadbackHasExactForeignKey(db *gorm.DB) bool {
	if db == nil || !db.Migrator().HasConstraint(&settlementReadbackLinkedLog{}, "SettlementBinding") {
		return false
	}
	var rows []settlementReadbackForeignKeyContract
	var err error
	switch db.Dialector.Name() {
	case "sqlite":
		err = db.Raw(`SELECT "table" AS referenced_table, "from" AS source_column,
			"to" AS referenced_column, UPPER(on_update) AS update_rule,
			UPPER(on_delete) AS delete_rule, 1 AS same_schema
			FROM pragma_foreign_key_list(?) WHERE "from" = ?`,
			"logs", "settlement_binding_id").Scan(&rows).Error
	case "mysql":
		err = db.Raw(`SELECT k.REFERENCED_TABLE_NAME AS referenced_table,
			k.COLUMN_NAME AS source_column,
			k.REFERENCED_COLUMN_NAME AS referenced_column,
			UPPER(r.UPDATE_RULE) AS update_rule,
			UPPER(r.DELETE_RULE) AS delete_rule,
			CASE WHEN k.REFERENCED_TABLE_SCHEMA = DATABASE() THEN 1 ELSE 0 END AS same_schema
			FROM information_schema.KEY_COLUMN_USAGE k
			JOIN information_schema.REFERENTIAL_CONSTRAINTS r
			  ON r.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
			 AND r.TABLE_NAME = k.TABLE_NAME
			 AND r.CONSTRAINT_NAME = k.CONSTRAINT_NAME
			WHERE k.CONSTRAINT_SCHEMA = DATABASE()
			  AND k.TABLE_NAME = ? AND k.CONSTRAINT_NAME = ?`,
			"logs", "fk_logs_settlement_binding").Scan(&rows).Error
	case "postgres":
		err = db.Raw(`SELECT ccu.table_name AS referenced_table,
			kcu.column_name AS source_column,
			ccu.column_name AS referenced_column,
			UPPER(rc.update_rule) AS update_rule,
			UPPER(rc.delete_rule) AS delete_rule,
			CASE WHEN ccu.table_schema = current_schema() THEN 1 ELSE 0 END AS same_schema
			FROM information_schema.referential_constraints rc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_catalog = rc.constraint_catalog
			 AND kcu.constraint_schema = rc.constraint_schema
			 AND kcu.constraint_name = rc.constraint_name
			JOIN information_schema.constraint_column_usage ccu
			  ON ccu.constraint_catalog = rc.unique_constraint_catalog
			 AND ccu.constraint_schema = rc.unique_constraint_schema
			 AND ccu.constraint_name = rc.unique_constraint_name
			WHERE kcu.table_schema = current_schema()
			  AND kcu.table_name = ? AND kcu.constraint_name = ?`,
			"logs", "fk_logs_settlement_binding").Scan(&rows).Error
	default:
		return false
	}
	if err != nil || len(rows) != 1 {
		return false
	}
	contract := rows[0]
	return contract.ReferencedTable == "settlement_readback_bindings" &&
		contract.SourceColumn == "settlement_binding_id" &&
		contract.ReferencedColumn == "id" &&
		contract.UpdateRule == "RESTRICT" &&
		contract.DeleteRule == "RESTRICT" &&
		contract.SameSchema == 1
}

func settlementReadbackHasExactUniqueIndex(db *gorm.DB, table any, indexName string, expectedColumns []string) bool {
	if db == nil || len(expectedColumns) == 0 {
		return false
	}
	if db.Dialector.Name() == "sqlite" {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(table); err != nil || statement.Schema == nil {
			return false
		}
		var indexes []struct {
			Name   string `gorm:"column:name"`
			Unique int    `gorm:"column:is_unique"`
		}
		if err := db.Raw(
			`SELECT name, "unique" AS is_unique FROM pragma_index_list(?) WHERE name = ?`,
			statement.Schema.Table,
			indexName,
		).Scan(&indexes).Error; err != nil || len(indexes) != 1 || indexes[0].Name != indexName || indexes[0].Unique != 1 {
			return false
		}
		var columns []struct {
			Name string `gorm:"column:name"`
		}
		if err := db.Raw(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, indexName).Scan(&columns).Error; err != nil || len(columns) != len(expectedColumns) {
			return false
		}
		for index := range columns {
			if columns[index].Name != expectedColumns[index] {
				return false
			}
		}
		return true
	}

	indexes, err := db.Migrator().GetIndexes(table)
	if err != nil {
		return false
	}
	for _, index := range indexes {
		if index.Name() != indexName {
			continue
		}
		unique, known := index.Unique()
		columns := index.Columns()
		if !known || !unique || len(columns) != len(expectedColumns) {
			return false
		}
		for position := range columns {
			if columns[position] != expectedColumns[position] {
				return false
			}
		}
		return true
	}
	return false
}
