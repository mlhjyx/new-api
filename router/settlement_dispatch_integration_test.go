package router

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type settlementDispatchRouteCase struct {
	name             string
	path             string
	channelType      int
	channelSettings  string
	requestBody      string
	wantUpstreamPath string
	upstreamBody     string
	conversionPolicy *model_setting.ChatCompletionsToResponsesPolicy
}

func TestSettlementDispatchSupportedRoutesUseOneAuthenticatedBindingAndOnePhysicalWire(t *testing.T) {
	cases := []settlementDispatchRouteCase{
		{
			name:             "chat completions",
			path:             "/v1/chat/completions",
			channelType:      constant.ChannelTypeOpenAI,
			requestBody:      `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath: "/v1/chat/completions",
			upstreamBody:     `{"id":"chatcmpl-direct","object":"chat.completion","created":1710000000,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		},
		{
			name:             "responses",
			path:             "/v1/responses",
			channelType:      constant.ChannelTypeOpenAI,
			requestBody:      `{"model":"gpt-4o-mini","input":"hello"}`,
			wantUpstreamPath: "/v1/responses",
			upstreamBody:     settlementResponsesBody("resp-direct"),
		},
		{
			name:             "anthropic messages",
			path:             "/v1/messages",
			channelType:      constant.ChannelTypeAnthropic,
			requestBody:      `{"model":"claude-3-haiku-20240307","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath: "/v1/messages",
			upstreamBody:     `{"id":"msg-direct","type":"message","role":"assistant","model":"claude-3-haiku-20240307","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		{
			name:             "chat completions via responses",
			path:             "/v1/chat/completions",
			channelType:      constant.ChannelTypeOpenAI,
			requestBody:      `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath: "/v1/responses",
			upstreamBody:     settlementResponsesBody("resp-chat-bridge"),
			conversionPolicy: &model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-4o-mini$"}},
		},
		{
			name:        "responses via chat completions",
			path:        "/v1/responses",
			channelType: constant.ChannelTypeAdvancedCustom,
			channelSettings: `{"advanced_custom":{"advanced_routes":[{` +
				`"incoming_path":"/v1/responses","upstream_path":"/v1/chat/completions",` +
				`"converter":"openai_responses_to_openai_chat_completions"}]}}`,
			requestBody:      `{"model":"gpt-4o-mini","input":"hello"}`,
			wantUpstreamPath: "/v1/chat/completions",
			upstreamBody:     `{"id":"chatcmpl-responses-bridge","object":"chat.completion","created":1710000000,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var physicalWires atomic.Int32
			var leakedSettlementHeader atomic.Bool
			var observedPath atomic.Value
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				physicalWires.Add(1)
				observedPath.Store(request.URL.Path)
				if request.Header.Get(common.SettlementReadbackRequestIdHeader) != "" || request.Header.Get(common.SettlementReadbackNonceHeader) != "" {
					leakedSettlementHeader.Store(true)
				}
				writer.Header().Set("Content-Type", "application/json")
				writer.Header().Set(common.RequestIdKey, "upstream-request-id")
				_, _ = writer.Write([]byte(testCase.upstreamBody))
			}))
			defer upstream.Close()

			db, engine := newSettlementDispatchIntegrationHarness(t, fmt.Sprintf("supported-%d", index), upstream.URL, testCase.channelType, testCase.channelSettings)
			if testCase.conversionPolicy != nil {
				settings := model_setting.GetGlobalSettings()
				original := settings.ChatCompletionsToResponsesPolicy
				settings.ChatCompletionsToResponsesPolicy = *testCase.conversionPolicy
				t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = original })
			}

			requestID := settlementDispatchOpaque(byte(index + 1))
			nonce := settlementDispatchOpaque(byte(index + 21))
			response := performSettlementDispatchRequest(engine, http.MethodPost, testCase.path, testCase.requestBody, requestID, nonce)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.EqualValues(t, 1, physicalWires.Load())
			assert.False(t, leakedSettlementHeader.Load())
			assert.Equal(t, testCase.wantUpstreamPath, observedPath.Load())

			var binding model.SettlementReadbackBinding
			require.NoError(t, db.Where("dispatch_token_id = ?", 42).First(&binding).Error)
			assert.Equal(t, model.SettlementReadbackBindingLogLinked, binding.State)
			var linkedLogs int64
			require.NoError(t, db.Model(&model.Log{}).Where("settlement_binding_id = ?", binding.Id).Count(&linkedLogs).Error)
			assert.EqualValues(t, 1, linkedLogs)
			var dispatchCAS int64
			require.NoError(t, db.Table("settlement_dispatch_audit").Count(&dispatchCAS).Error)
			assert.EqualValues(t, 1, dispatchCAS)

			replay := performSettlementDispatchRequest(engine, http.MethodPost, testCase.path, testCase.requestBody, requestID, nonce)
			assert.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
			assert.EqualValues(t, 1, physicalWires.Load())
			require.NoError(t, db.Table("settlement_dispatch_audit").Count(&dispatchCAS).Error)
			assert.EqualValues(t, 1, dispatchCAS)
		})
	}
}

func TestSettlementDispatchRejectsUnsupportedAndAmbiguousRouterPathsWithoutRedirectOrWire(t *testing.T) {
	var physicalWires atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		physicalWires.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	db, engine := newSettlementDispatchIntegrationHarness(t, "rejected", upstream.URL, constant.ChannelTypeOpenAI, "")
	targets := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/chat/completions/"},
		{http.MethodPost, "/v1/chat/completions?mode=other"},
		{http.MethodPost, "//v1/chat/completions"},
		{http.MethodPost, "/v1/%63hat/completions"},
		{http.MethodPost, "/v1/chat/completions/../images/generations"},
		{http.MethodGet, "/v1/realtime"},
		{http.MethodPost, "/v1/realtime"},
		{http.MethodPost, "/v1beta/models/gemini:generateContent"},
		{http.MethodPost, "/v1/images/generations"},
		{http.MethodPost, "/v1/audio/speech"},
		{http.MethodPost, "/v1/rerank"},
		{http.MethodPost, "/v1/videos"},
		{http.MethodPost, "/mj/submit/imagine"},
		{http.MethodPost, "/suno/submit/generate"},
	}

	for index, target := range targets {
		response := performSettlementDispatchRequest(
			engine,
			target.method,
			target.path,
			`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`,
			settlementDispatchOpaque(byte(index+50)),
			settlementDispatchOpaque(byte(index+100)),
		)
		assert.NotContains(t, []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}, response.Code, target.path)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound}, response.Code, target.path)
	}
	assert.Zero(t, physicalWires.Load())
	var bindings int64
	require.NoError(t, db.Model(&model.SettlementReadbackBinding{}).Count(&bindings).Error)
	assert.Zero(t, bindings)
}

func newSettlementDispatchIntegrationHarness(t *testing.T, name string, upstreamURL string, channelType int, channelSettings string) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsnName := strings.NewReplacer(" ", "-", "/", "-").Replace(name)
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalConsume, originalExport := common.LogConsumeEnabled, common.DataExportEnabled
	originalRedis, originalMemoryCache := common.RedisEnabled, common.MemoryCacheEnabled
	originalRateLimit := setting.ModelRequestRateLimitEnabled
	originalSQLitePath, originalMaster := common.SQLitePath, common.IsMasterNode
	common.SQLitePath = filepath.Join(t.TempDir(), "settlement-router-"+dsnName+".db") + "?_busy_timeout=30000&_pragma=foreign_keys(1)"
	common.IsMasterNode = false
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}))
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(db))
	require.NoError(t, db.Exec(`CREATE TABLE settlement_dispatch_audit (id integer primary key autoincrement, binding_id integer not null)`).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER settlement_dispatch_started_audit
		AFTER UPDATE OF state ON settlement_readback_bindings
		WHEN OLD.state = 'BOUND' AND NEW.state = 'DISPATCH_STARTED'
		BEGIN
			INSERT INTO settlement_dispatch_audit(binding_id) VALUES (NEW.id);
		END`).Error)

	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	setting.ModelRequestRateLimitEnabled = false
	ratio_setting.InitRatioSettings()
	service.InitHttpClient()
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetMainDatabaseType(originalMainType)
		common.SetLogDatabaseType(originalLogType)
		common.LogConsumeEnabled, common.DataExportEnabled = originalConsume, originalExport
		common.RedisEnabled, common.MemoryCacheEnabled = originalRedis, originalMemoryCache
		setting.ModelRequestRateLimitEnabled = originalRateLimit
		common.SQLitePath, common.IsMasterNode = originalSQLitePath, originalMaster
	})

	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "settlement-root",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Quota:    1_000_000_000,
		Group:    "default",
		Setting:  `{"billing_preference":"wallet_only"}`,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:             42,
		UserId:         7,
		Key:            "settlementdispatch",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    1_000_000_000,
		UnlimitedQuota: true,
		Group:          "default",
	}).Error)
	require.NoError(t, db.Create(&model.SettlementReadbackCredential{
		LookupPrefix:    "readerlookup0001",
		SecretDigest:    strings.Repeat("a", 64),
		PepperVersion:   "pepper-v1",
		DispatchTokenId: 42,
		Status:          model.SettlementReadbackCredentialActive,
		CreatedAt:       1,
	}).Error)
	baseURL := upstreamURL
	require.NoError(t, db.Create(&model.Channel{
		Id:            7,
		Type:          channelType,
		Key:           "local-capture-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "local-capture",
		BaseURL:       &baseURL,
		Models:        "gpt-4o-mini,claude-3-haiku-20240307",
		Group:         "default",
		OtherSettings: channelSettings,
	}).Error)

	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	return db, engine
}

func performSettlementDispatchRequest(engine http.Handler, method string, target string, body string, requestID string, nonce string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer sk-settlementdispatch-7")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(common.SettlementReadbackRequestIdHeader, requestID)
	request.Header.Set(common.SettlementReadbackNonceHeader, nonce)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func settlementDispatchOpaque(fill byte) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func settlementResponsesBody(id string) string {
	return `{"id":"` + id + `","object":"response","created_at":1710000000,"status":"completed","model":"gpt-4o-mini","output":[{"type":"message","id":"msg-1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`
}
