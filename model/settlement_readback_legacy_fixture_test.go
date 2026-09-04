package model

import (
	"database/sql"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// This fixture is frozen from the exact upstream Git object, not generated
// from the current Log model:
//
//	commit       bde9b2f44887d34ec54799ae191d50f97914359e
//	path         model/log.go
//	git blob     506bd504b68687714e1eefed70c45b1264fe6f88
//	blob sha256  e0f3e3217d51894cda56fc2aba7818caca8b222dc03e0c029e8c30b7bffb2823
var settlementReadbackLegacyLogColumns = []string{
	"id", "user_id", "created_at", "type", "content", "username", "token_name",
	"model_name", "quota", "prompt_tokens", "completion_tokens", "use_time",
	"is_stream", "channel_id", "channel_name", "token_id", "group", "ip",
	"request_id", "upstream_request_id", "other",
}

const settlementReadbackLegacyFixtureProvenance = "git:bde9b2f44887d34ec54799ae191d50f97914359e:model/log.go:blob-506bd504b68687714e1eefed70c45b1264fe6f88:sha256-e0f3e3217d51894cda56fc2aba7818caca8b222dc03e0c029e8c30b7bffb2823"

var settlementReadbackLegacyLogIndexes = map[string][]string{
	"idx_created_at_id":            {"created_at", "id"},
	"idx_created_at_type":          {"created_at", "type"},
	"idx_logs_channel_id":          {"channel_id"},
	"idx_logs_group":               {"group"},
	"idx_logs_ip":                  {"ip"},
	"idx_logs_model_name":          {"model_name"},
	"idx_logs_request_id":          {"request_id"},
	"idx_logs_token_id":            {"token_id"},
	"idx_logs_token_name":          {"token_name"},
	"idx_logs_upstream_request_id": {"upstream_request_id"},
	"idx_logs_user_id":             {"user_id"},
	"idx_logs_username":            {"username"},
	"idx_user_id_id":               {"user_id", "id"},
	"index_username_model_name":    {"model_name", "username"},
}

func settlementReadbackLegacyLogDDL(databaseType common.DatabaseType) []string {
	switch databaseType {
	case common.DatabaseTypeSQLite:
		return []string{
			"CREATE TABLE `logs` (`id` integer,`user_id` integer,`created_at` integer,`type` integer,`content` text," +
				"`username` text DEFAULT \"\",`token_name` text DEFAULT \"\",`model_name` text DEFAULT \"\"," +
				"`quota` integer DEFAULT 0,`prompt_tokens` integer DEFAULT 0,`completion_tokens` integer DEFAULT 0," +
				"`use_time` integer DEFAULT 0,`is_stream` numeric,`channel_id` integer,`channel_name` text," +
				"`token_id` integer DEFAULT 0,`group` text,`ip` text DEFAULT \"\",`request_id` varchar(64) DEFAULT \"\"," +
				"`upstream_request_id` varchar(128) DEFAULT \"\",`other` text,PRIMARY KEY (`id`))",
			`CREATE INDEX idx_created_at_id ON logs(created_at, id)`,
			`CREATE INDEX idx_created_at_type ON logs(created_at, type)`,
			`CREATE INDEX idx_logs_channel_id ON logs(channel_id)`,
			`CREATE INDEX idx_logs_group ON logs("group")`,
			`CREATE INDEX idx_logs_ip ON logs(ip)`,
			`CREATE INDEX idx_logs_model_name ON logs(model_name)`,
			`CREATE INDEX idx_logs_request_id ON logs(request_id)`,
			`CREATE INDEX idx_logs_token_id ON logs(token_id)`,
			`CREATE INDEX idx_logs_token_name ON logs(token_name)`,
			`CREATE INDEX idx_logs_upstream_request_id ON logs(upstream_request_id)`,
			`CREATE INDEX idx_logs_user_id ON logs(user_id)`,
			`CREATE INDEX idx_logs_username ON logs(username)`,
			`CREATE INDEX idx_user_id_id ON logs(user_id, id)`,
			`CREATE INDEX index_username_model_name ON logs(model_name, username)`,
		}
	case common.DatabaseTypeMySQL:
		return []string{
			`CREATE TABLE logs (
				id BIGINT NOT NULL AUTO_INCREMENT,
				user_id BIGINT DEFAULT NULL,
				created_at BIGINT DEFAULT NULL,
				type BIGINT DEFAULT NULL,
				content LONGTEXT,
				username VARCHAR(191) DEFAULT '',
				token_name VARCHAR(191) DEFAULT '',
				model_name VARCHAR(191) DEFAULT '',
				quota BIGINT DEFAULT 0,
				prompt_tokens BIGINT DEFAULT 0,
				completion_tokens BIGINT DEFAULT 0,
				use_time BIGINT DEFAULT 0,
				is_stream TINYINT(1) DEFAULT NULL,
				channel_id BIGINT DEFAULT NULL,
				channel_name LONGTEXT,
				token_id BIGINT DEFAULT 0,
				` + "`group`" + ` VARCHAR(191) DEFAULT NULL,
				ip VARCHAR(191) DEFAULT '',
				request_id VARCHAR(64) DEFAULT '',
				upstream_request_id VARCHAR(128) DEFAULT '',
				other LONGTEXT,
				PRIMARY KEY (id),
				KEY idx_created_at_id (created_at, id),
				KEY idx_created_at_type (created_at, type),
				KEY idx_logs_channel_id (channel_id),
				KEY idx_logs_group (` + "`group`" + `),
				KEY idx_logs_ip (ip),
				KEY idx_logs_model_name (model_name),
				KEY idx_logs_request_id (request_id),
				KEY idx_logs_token_id (token_id),
				KEY idx_logs_token_name (token_name),
				KEY idx_logs_upstream_request_id (upstream_request_id),
				KEY idx_logs_user_id (user_id),
				KEY idx_logs_username (username),
				KEY idx_user_id_id (user_id, id),
				KEY index_username_model_name (model_name, username)
			) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		}
	case common.DatabaseTypePostgreSQL:
		return []string{
			`CREATE TABLE logs (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT,
				created_at BIGINT,
				type BIGINT,
				content TEXT,
				username TEXT DEFAULT '',
				token_name TEXT DEFAULT '',
				model_name TEXT DEFAULT '',
				quota BIGINT DEFAULT 0,
				prompt_tokens BIGINT DEFAULT 0,
				completion_tokens BIGINT DEFAULT 0,
				use_time BIGINT DEFAULT 0,
				is_stream BOOLEAN,
				channel_id BIGINT,
				channel_name TEXT,
				token_id BIGINT DEFAULT 0,
				"group" TEXT,
				ip TEXT DEFAULT '',
				request_id VARCHAR(64) DEFAULT '',
				upstream_request_id VARCHAR(128) DEFAULT '',
				other TEXT
			)`,
			`CREATE INDEX idx_created_at_id ON logs(created_at, id)`,
			`CREATE INDEX idx_created_at_type ON logs(created_at, type)`,
			`CREATE INDEX idx_logs_channel_id ON logs(channel_id)`,
			`CREATE INDEX idx_logs_group ON logs("group")`,
			`CREATE INDEX idx_logs_ip ON logs(ip)`,
			`CREATE INDEX idx_logs_model_name ON logs(model_name)`,
			`CREATE INDEX idx_logs_request_id ON logs(request_id)`,
			`CREATE INDEX idx_logs_token_id ON logs(token_id)`,
			`CREATE INDEX idx_logs_token_name ON logs(token_name)`,
			`CREATE INDEX idx_logs_upstream_request_id ON logs(upstream_request_id)`,
			`CREATE INDEX idx_logs_user_id ON logs(user_id)`,
			`CREATE INDEX idx_logs_username ON logs(username)`,
			`CREATE INDEX idx_user_id_id ON logs(user_id, id)`,
			`CREATE INDEX index_username_model_name ON logs(model_name, username)`,
		}
	default:
		return nil
	}
}

type settlementReadbackLegacyColumnSnapshot struct {
	Name          string
	DatabaseType  string
	ColumnType    string
	ColumnKnown   bool
	Nullable      bool
	NullableKnown bool
	Default       string
	DefaultKnown  bool
	Primary       bool
	PrimaryKnown  bool
	AutoIncrement bool
	AutoKnown     bool
	Length        int64
	LengthKnown   bool
}

type settlementReadbackLegacySchemaSnapshot struct {
	Columns []settlementReadbackLegacyColumnSnapshot
	Indexes map[string][]string
}

type settlementReadbackLegacyFullRow struct {
	ID                int64  `gorm:"column:id"`
	UserID            int64  `gorm:"column:user_id"`
	CreatedAt         int64  `gorm:"column:created_at"`
	Type              int64  `gorm:"column:type"`
	Content           string `gorm:"column:content"`
	Username          string `gorm:"column:username"`
	TokenName         string `gorm:"column:token_name"`
	ModelName         string `gorm:"column:model_name"`
	Quota             int64  `gorm:"column:quota"`
	PromptTokens      int64  `gorm:"column:prompt_tokens"`
	CompletionTokens  int64  `gorm:"column:completion_tokens"`
	UseTime           int64  `gorm:"column:use_time"`
	IsStream          bool   `gorm:"column:is_stream"`
	ChannelID         int64  `gorm:"column:channel_id"`
	ChannelName       string `gorm:"column:channel_name"`
	TokenID           int64  `gorm:"column:token_id"`
	Group             string `gorm:"column:group"`
	IP                string `gorm:"column:ip"`
	RequestID         string `gorm:"column:request_id"`
	UpstreamRequestID string `gorm:"column:upstream_request_id"`
	Other             string `gorm:"column:other"`
}

type settlementReadbackLegacyDefaultRow struct {
	ID                int64          `gorm:"column:id"`
	UserID            sql.NullInt64  `gorm:"column:user_id"`
	CreatedAt         sql.NullInt64  `gorm:"column:created_at"`
	Type              int64          `gorm:"column:type"`
	Content           sql.NullString `gorm:"column:content"`
	Username          string         `gorm:"column:username"`
	TokenName         string         `gorm:"column:token_name"`
	ModelName         string         `gorm:"column:model_name"`
	Quota             int64          `gorm:"column:quota"`
	PromptTokens      int64          `gorm:"column:prompt_tokens"`
	CompletionTokens  int64          `gorm:"column:completion_tokens"`
	UseTime           int64          `gorm:"column:use_time"`
	IsStream          sql.NullBool   `gorm:"column:is_stream"`
	ChannelID         sql.NullInt64  `gorm:"column:channel_id"`
	ChannelName       sql.NullString `gorm:"column:channel_name"`
	TokenID           int64          `gorm:"column:token_id"`
	Group             sql.NullString `gorm:"column:group"`
	IP                string         `gorm:"column:ip"`
	RequestID         string         `gorm:"column:request_id"`
	UpstreamRequestID string         `gorm:"column:upstream_request_id"`
	Other             sql.NullString `gorm:"column:other"`
}

func createSettlementReadbackLegacyLogFixture(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	statements := settlementReadbackLegacyLogDDL(databaseType)
	require.NotEmpty(t, statements)
	for _, statement := range statements {
		require.NoError(t, db.Exec(statement).Error)
	}
	assertSettlementReadbackLegacyManifest(t, db, databaseType)
}

func assertSettlementReadbackLegacyManifest(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	snapshot := captureSettlementReadbackLegacySchema(t, db, databaseType)
	actualColumns := make([]string, 0, len(snapshot.Columns))
	for _, column := range snapshot.Columns {
		actualColumns = append(actualColumns, column.Name)
	}
	assert.Equal(t, settlementReadbackLegacyLogColumns, actualColumns, settlementReadbackLegacyFixtureProvenance)
	assert.Equal(t, settlementReadbackLegacyLogIndexes, snapshot.Indexes, settlementReadbackLegacyFixtureProvenance)
}

func captureSettlementReadbackLegacySchema(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) settlementReadbackLegacySchemaSnapshot {
	t.Helper()
	columnTypes, err := db.Migrator().ColumnTypes("logs")
	require.NoError(t, err)
	columns := make([]settlementReadbackLegacyColumnSnapshot, 0, len(settlementReadbackLegacyLogColumns))
	legacyNames := make(map[string]struct{}, len(settlementReadbackLegacyLogColumns))
	for _, name := range settlementReadbackLegacyLogColumns {
		legacyNames[name] = struct{}{}
	}
	for _, columnType := range columnTypes {
		name := strings.ToLower(columnType.Name())
		if _, expected := legacyNames[name]; !expected {
			continue
		}
		columnDDL, columnKnown := columnType.ColumnType()
		nullable, nullableKnown := columnType.Nullable()
		defaultValue, defaultKnown := columnType.DefaultValue()
		primary, primaryKnown := columnType.PrimaryKey()
		auto, autoKnown := columnType.AutoIncrement()
		length, lengthKnown := columnType.Length()
		columns = append(columns, settlementReadbackLegacyColumnSnapshot{
			Name: name, DatabaseType: strings.ToLower(columnType.DatabaseTypeName()),
			ColumnType: strings.ToLower(columnDDL), ColumnKnown: columnKnown,
			Nullable: nullable, NullableKnown: nullableKnown,
			Default: defaultValue, DefaultKnown: defaultKnown, Primary: primary, PrimaryKnown: primaryKnown,
			AutoIncrement: auto, AutoKnown: autoKnown, Length: length, LengthKnown: lengthKnown,
		})
	}
	order := make(map[string]int, len(settlementReadbackLegacyLogColumns))
	for index, name := range settlementReadbackLegacyLogColumns {
		order[name] = index
	}
	sort.Slice(columns, func(left int, right int) bool { return order[columns[left].Name] < order[columns[right].Name] })
	return settlementReadbackLegacySchemaSnapshot{Columns: columns, Indexes: captureSettlementReadbackLegacyIndexes(t, db, databaseType)}
}

func captureSettlementReadbackLegacyIndexes(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) map[string][]string {
	t.Helper()
	result := make(map[string][]string, len(settlementReadbackLegacyLogIndexes))
	for name, expectedColumns := range settlementReadbackLegacyLogIndexes {
		columns, unique, complete := inspectSettlementReadbackLegacyIndex(db, databaseType, name)
		require.Truef(t, complete, "legacy index %s is not introspectable", name)
		assert.Falsef(t, unique, "legacy index %s unexpectedly became unique", name)
		assert.Equal(t, expectedColumns, columns, "legacy index %s changed", name)
		result[name] = columns
	}
	return result
}

func inspectSettlementReadbackLegacyIndex(db *gorm.DB, databaseType common.DatabaseType, indexName string) ([]string, bool, bool) {
	var rows []struct {
		ColumnName string `gorm:"column:column_name"`
		IsUnique   bool   `gorm:"column:is_unique"`
	}
	var err error
	switch databaseType {
	case common.DatabaseTypeSQLite:
		err = db.Raw(`SELECT ii.name AS column_name, (il."unique" = 1) AS is_unique
			FROM pragma_index_list('logs', 'main') il
			JOIN pragma_index_info(il.name, 'main') ii
			WHERE il.name = ? ORDER BY ii.seqno`, indexName).Scan(&rows).Error
	case common.DatabaseTypeMySQL:
		err = db.Raw(`SELECT COLUMN_NAME AS column_name, (NON_UNIQUE = 0) AS is_unique
			FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'logs' AND INDEX_NAME = ?
			ORDER BY SEQ_IN_INDEX`, indexName).Scan(&rows).Error
	case common.DatabaseTypePostgreSQL:
		err = db.Raw(`SELECT a.attname AS column_name, ix.indisunique AS is_unique
			FROM pg_catalog.pg_class t
			JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
			JOIN pg_catalog.pg_index ix ON ix.indrelid = t.oid
			JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
			JOIN LATERAL unnest(ix.indkey::smallint[]) WITH ORDINALITY keys(attnum, ordinality) ON true
			JOIN pg_catalog.pg_attribute a ON a.attrelid = t.oid AND a.attnum = keys.attnum
			WHERE n.nspname = current_schema() AND t.relname = 'logs' AND i.relname = ?
			ORDER BY keys.ordinality`, indexName).Scan(&rows).Error
	default:
		return nil, false, false
	}
	if err != nil || len(rows) == 0 {
		return nil, false, false
	}
	columns := make([]string, 0, len(rows))
	unique := rows[0].IsUnique
	for _, row := range rows {
		columns = append(columns, row.ColumnName)
		if row.IsUnique != unique {
			return nil, false, false
		}
	}
	return columns, unique, true
}

func seedSettlementReadbackLegacyRows(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) (settlementReadbackLegacyFullRow, settlementReadbackLegacyDefaultRow) {
	t.Helper()
	full := settlementReadbackLegacyFullRow{
		ID: math.MaxInt64 - 20, UserID: math.MaxInt64 - 21, CreatedAt: math.MaxInt64 - 22,
		Type: LogTypeConsume, Content: "legacy-边界-content", Username: strings.Repeat("u", 191),
		TokenName: strings.Repeat("t", 191), ModelName: strings.Repeat("m", 191),
		Quota: math.MaxInt64, PromptTokens: math.MaxInt64 - 23, CompletionTokens: 0,
		UseTime: math.MaxInt64 - 24, IsStream: true, ChannelID: math.MaxInt64 - 25,
		ChannelName: "legacy-channel", TokenID: 4_242, Group: strings.Repeat("g", 191),
		IP: strings.Repeat("i", 191), RequestID: strings.Repeat("r", 64),
		UpstreamRequestID: strings.Repeat("p", 128),
		Other:             `{"usage_semantic":"openai","admin_info":{"private":"redacted"}}`,
	}
	groupColumn := `"group"`
	if databaseType == common.DatabaseTypeMySQL {
		groupColumn = "`group`"
	}
	require.NoError(t, db.Exec(`INSERT INTO logs (
		id, user_id, created_at, type, content, username, token_name, model_name,
		quota, prompt_tokens, completion_tokens, use_time, is_stream, channel_id,
		channel_name, token_id, `+groupColumn+`, ip, request_id, upstream_request_id, other
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		full.ID, full.UserID, full.CreatedAt, full.Type, full.Content, full.Username,
		full.TokenName, full.ModelName, full.Quota, full.PromptTokens,
		full.CompletionTokens, full.UseTime, full.IsStream, full.ChannelID,
		full.ChannelName, full.TokenID, full.Group, full.IP, full.RequestID,
		full.UpstreamRequestID, full.Other,
	).Error)

	defaultRow := settlementReadbackLegacyDefaultRow{ID: math.MaxInt64 - 10, Type: LogTypeError, TokenID: 0, RequestID: ""}
	require.NoError(t, db.Exec("INSERT INTO logs (id, type) VALUES (?, ?)", defaultRow.ID, defaultRow.Type).Error)
	return full, defaultRow
}

func assertSettlementReadbackLegacyRows(t *testing.T, db *gorm.DB, databaseType common.DatabaseType, full settlementReadbackLegacyFullRow, defaultExpected settlementReadbackLegacyDefaultRow) {
	t.Helper()
	var count int64
	require.NoError(t, db.Table("logs").Count(&count).Error)
	assert.EqualValues(t, 2, count)
	groupColumn := `"group"`
	if databaseType == common.DatabaseTypeMySQL {
		groupColumn = "`group`"
	}
	var fullStored settlementReadbackLegacyFullRow
	require.NoError(t, db.Raw(`SELECT id, user_id, created_at, type, content, username,
		token_name, model_name, quota, prompt_tokens, completion_tokens, use_time,
		is_stream, channel_id, channel_name, token_id, `+groupColumn+`, ip, request_id,
		upstream_request_id, other FROM logs WHERE id = ?`, full.ID).Scan(&fullStored).Error)
	assert.Equal(t, full, fullStored)

	var defaultStored settlementReadbackLegacyDefaultRow
	require.NoError(t, db.Raw(`SELECT id, user_id, created_at, type, content, username,
		token_name, model_name, quota, prompt_tokens, completion_tokens, use_time,
		is_stream, channel_id, channel_name, token_id, `+groupColumn+`, ip, request_id,
		upstream_request_id, other FROM logs WHERE id = ?`, defaultExpected.ID).Scan(&defaultStored).Error)
	assert.Equal(t, defaultExpected.ID, defaultStored.ID)
	assert.Equal(t, defaultExpected.Type, defaultStored.Type)
	assert.False(t, defaultStored.UserID.Valid)
	assert.False(t, defaultStored.CreatedAt.Valid)
	assert.False(t, defaultStored.Content.Valid)
	assert.False(t, defaultStored.IsStream.Valid)
	assert.False(t, defaultStored.ChannelID.Valid)
	assert.False(t, defaultStored.ChannelName.Valid)
	assert.False(t, defaultStored.Group.Valid)
	assert.False(t, defaultStored.Other.Valid)
	assert.Empty(t, defaultStored.Username)
	assert.Empty(t, defaultStored.TokenName)
	assert.Empty(t, defaultStored.ModelName)
	assert.Zero(t, defaultStored.Quota)
	assert.Zero(t, defaultStored.PromptTokens)
	assert.Zero(t, defaultStored.CompletionTokens)
	assert.Zero(t, defaultStored.UseTime)
	assert.Zero(t, defaultStored.TokenID)
	assert.Empty(t, defaultStored.IP)
	assert.Empty(t, defaultStored.RequestID)
	assert.Empty(t, defaultStored.UpstreamRequestID)
}

func assertSettlementReadbackLegacyReadPath(t *testing.T, db *gorm.DB, databaseType common.DatabaseType, full settlementReadbackLegacyFullRow) {
	t.Helper()
	restoreSettlementReadbackTestTopology(t, db, databaseType)
	logs, err := GetLogByTokenId(int(full.TokenID))
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, full.ModelName, logs[0].ModelName)
	assert.Equal(t, int(full.UseTime), logs[0].UseTime)
	assert.True(t, logs[0].IsStream)
	assert.Equal(t, full.Group, logs[0].Group)
	assert.NotContains(t, logs[0].Other, "admin_info")
	assert.Contains(t, logs[0].Other, "usage_semantic")
}

func assertSettlementReadbackLegacyMigration(t *testing.T, db *gorm.DB, databaseType common.DatabaseType) {
	t.Helper()
	createSettlementReadbackLegacyLogFixture(t, db, databaseType)
	before := captureSettlementReadbackLegacySchema(t, db, databaseType)
	full, defaultRow := seedSettlementReadbackLegacyRows(t, db, databaseType)

	require.NoError(t, EnsureSettlementReadbackSharedSchema(db))
	after := captureSettlementReadbackLegacySchema(t, db, databaseType)
	assert.Equal(t, before, after, "the additive settlement migration must not rewrite any legacy column/default/index")
	assertSettlementReadbackLegacyRows(t, db, databaseType, full, defaultRow)
	assertSettlementReadbackLegacyReadPath(t, db, databaseType, full)

	var unlinked int64
	require.NoError(t, db.Model(&Log{}).Where("settlement_binding_id IS NULL").Count(&unlinked).Error)
	assert.EqualValues(t, 2, unlinked)
}
