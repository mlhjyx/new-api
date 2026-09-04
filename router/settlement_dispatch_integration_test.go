package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	name              string
	path              string
	channelType       int
	channelSettings   string
	requestBody       string
	wantUpstreamPath  string
	upstreamBody      string
	upstreamMediaType string
	conversionPolicy  *model_setting.ChatCompletionsToResponsesPolicy
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
		{
			name:              "streaming chat completions",
			path:              "/v1/chat/completions",
			channelType:       constant.ChannelTypeOpenAI,
			requestBody:       `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath:  "/v1/chat/completions",
			upstreamBody:      settlementChatStreamBody("chatcmpl-stream"),
			upstreamMediaType: "text/event-stream",
		},
		{
			name:              "streaming responses",
			path:              "/v1/responses",
			channelType:       constant.ChannelTypeOpenAI,
			requestBody:       `{"model":"gpt-4o-mini","stream":true,"input":"hello"}`,
			wantUpstreamPath:  "/v1/responses",
			upstreamBody:      settlementResponsesStreamBody("resp-stream"),
			upstreamMediaType: "text/event-stream",
		},
		{
			name:              "streaming anthropic messages",
			path:              "/v1/messages",
			channelType:       constant.ChannelTypeAnthropic,
			requestBody:       `{"model":"claude-3-haiku-20240307","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath:  "/v1/messages",
			upstreamBody:      settlementClaudeStreamBody(),
			upstreamMediaType: "text/event-stream",
		},
		{
			name:              "streaming chat completions via responses",
			path:              "/v1/chat/completions",
			channelType:       constant.ChannelTypeOpenAI,
			requestBody:       `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`,
			wantUpstreamPath:  "/v1/responses",
			upstreamBody:      settlementResponsesStreamBody("resp-chat-stream-bridge"),
			upstreamMediaType: "text/event-stream",
			conversionPolicy:  &model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-4o-mini$"}},
		},
		{
			name:        "streaming responses via chat completions",
			path:        "/v1/responses",
			channelType: constant.ChannelTypeAdvancedCustom,
			channelSettings: `{"advanced_custom":{"advanced_routes":[{` +
				`"incoming_path":"/v1/responses","upstream_path":"/v1/chat/completions",` +
				`"converter":"openai_responses_to_openai_chat_completions"}]}}`,
			requestBody:       `{"model":"gpt-4o-mini","stream":true,"input":"hello"}`,
			wantUpstreamPath:  "/v1/chat/completions",
			upstreamBody:      settlementChatStreamBody("chatcmpl-responses-stream-bridge"),
			upstreamMediaType: "text/event-stream",
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
				mediaType := testCase.upstreamMediaType
				if mediaType == "" {
					mediaType = "application/json"
				}
				writer.Header().Set("Content-Type", mediaType)
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

func TestSettlementDispatchRejectsAdaptersOutsideCommonCASWithoutFallbackWire(t *testing.T) {
	tests := []struct {
		name            string
		channelType     int
		channelKey      string
		channelSettings func(string) string
	}{
		{
			name:        "xunfei websocket in DoResponse",
			channelType: constant.ChannelTypeXunfei,
			channelKey:  "test-app|test-signing-material|test-api-key",
		},
		{
			name:        "aws sdk InvokeModel in DoResponse",
			channelType: constant.ChannelTypeAws,
			channelKey:  "test-access|test-signing-material|us-east-1",
			channelSettings: func(proxyURL string) string {
				return `{"proxy":"` + proxyURL + `"}`
			},
		},
	}

	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var unsupportedPhysicalActions atomic.Int32
			unsupportedCapture := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				unsupportedPhysicalActions.Add(1)
				writer.WriteHeader(http.StatusBadGateway)
			}))
			defer unsupportedCapture.Close()

			var fallbackWires atomic.Int32
			fallback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				fallbackWires.Add(1)
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"id":"chatcmpl-fallback","object":"chat.completion","created":1710000000,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"must-not-run"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			}))
			defer fallback.Close()

			db, engine := newSettlementDispatchIntegrationHarness(t, fmt.Sprintf("adapter-deny-%d", index), unsupportedCapture.URL, testCase.channelType, "")
			runtimeSettings := ""
			if testCase.channelSettings != nil {
				runtimeSettings = testCase.channelSettings(unsupportedCapture.URL)
			}
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7).Updates(map[string]any{
				"key":     testCase.channelKey,
				"setting": runtimeSettings,
			}).Error)
			fallbackURL := fallback.URL
			fallbackPriority := int64(0)
			require.NoError(t, db.Create(&model.Channel{
				Id:       8,
				Type:     constant.ChannelTypeOpenAI,
				Key:      "local-fallback-key",
				Status:   common.ChannelStatusEnabled,
				Name:     "local-fallback",
				BaseURL:  &fallbackURL,
				Models:   "gpt-4o-mini",
				Group:    "default",
				Priority: &fallbackPriority,
			}).Error)
			require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4o-mini", ChannelId: 8, Enabled: true, Priority: &fallbackPriority}).Error)

			originalRetries := common.RetryTimes
			common.RetryTimes = 1
			t.Cleanup(func() { common.RetryTimes = originalRetries })

			requestID := settlementDispatchOpaque(byte(index + 150))
			nonce := settlementDispatchOpaque(byte(index + 160))
			response := performSettlementDispatchRequest(engine, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`, requestID, nonce)
			assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), `"code":"settlement_dispatch_fence_unavailable"`)
			assert.Zero(t, unsupportedPhysicalActions.Load())
			assert.Zero(t, fallbackWires.Load(), "an unfenced adapter must not fall through to a second channel")

			var dispatchCAS int64
			require.NoError(t, db.Table("settlement_dispatch_audit").Count(&dispatchCAS).Error)
			assert.Zero(t, dispatchCAS)
			var linkedLogs int64
			require.NoError(t, db.Model(&model.Log{}).Where("settlement_binding_id IS NOT NULL").Count(&linkedLogs).Error)
			assert.Zero(t, linkedLogs)

			replay := performSettlementDispatchRequest(engine, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`, requestID, nonce)
			assert.Equal(t, http.StatusServiceUnavailable, replay.Code, replay.Body.String())
			assert.Zero(t, unsupportedPhysicalActions.Load())
			assert.Zero(t, fallbackWires.Load())
			require.NoError(t, db.Table("settlement_dispatch_audit").Count(&dispatchCAS).Error)
			assert.Zero(t, dispatchCAS)
		})
	}
}

func TestSettlementTrailingSlashRedirectIsPreservedOnlyForUnboundTokens(t *testing.T) {
	var physicalWires atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		physicalWires.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	db, engine := newSettlementDispatchIntegrationHarness(t, "trailing-slash-redirect", upstream.URL, constant.ChannelTypeOpenAI, "")
	require.NoError(t, db.Create(&model.Token{
		Id:             43,
		UserId:         7,
		Key:            "ordinarydispatch",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    1_000_000_000,
		UnlimitedQuota: true,
		Group:          "default",
	}).Error)

	ordinary := performSettlementDispatchRequestWithAuthorization(
		engine,
		http.MethodPost,
		"/v1/chat/completions/",
		`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`,
		"Bearer sk-ordinarydispatch",
		"",
		"",
	)
	assert.Equal(t, http.StatusTemporaryRedirect, ordinary.Code)
	assert.Equal(t, "/v1/chat/completions", ordinary.Header().Get("Location"))

	otherRoute := performSettlementDispatchRequestWithAuthorization(
		engine,
		http.MethodGet,
		"/v1/models/",
		"",
		"Bearer sk-ordinarydispatch",
		"",
		"",
	)
	assert.Equal(t, http.StatusMovedPermanently, otherRoute.Code)
	assert.Equal(t, "/v1/models", otherRoute.Header().Get("Location"))

	assert.Zero(t, physicalWires.Load())
	var bindings int64
	require.NoError(t, db.Model(&model.SettlementReadbackBinding{}).Count(&bindings).Error)
	assert.Zero(t, bindings)
}

func TestSettlementDispatchStreamUncertaintyLeavesOneWireAndNoReceipt(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "client disconnect", mode: "client_disconnect"},
		{name: "upstream interruption before terminal event", mode: "upstream_interruption"},
		{name: "post-wire log persistence failure", mode: "log_persistence_failure"},
	}

	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			inboundContext, cancelInbound := context.WithCancel(context.Background())
			defer cancelInbound()
			var primaryWires atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				primaryWires.Add(1)
				writer.Header().Set("Content-Type", "text/event-stream")
				switch testCase.mode {
				case "client_disconnect":
					_, _ = writer.Write([]byte(settlementChatStreamChunk("chatcmpl-client-gone")))
					if flusher, ok := writer.(http.Flusher); ok {
						flusher.Flush()
					}
					cancelInbound()
					select {
					case <-request.Context().Done():
					case <-time.After(2 * time.Second):
					}
				case "upstream_interruption":
					_, _ = writer.Write([]byte(settlementChatStreamChunk("chatcmpl-truncated")))
				case "log_persistence_failure":
					_, _ = writer.Write([]byte(settlementChatStreamBody("chatcmpl-log-failure")))
				}
			}))
			defer primary.Close()

			var fallbackWires atomic.Int32
			fallback := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				fallbackWires.Add(1)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(settlementChatStreamBody("chatcmpl-forbidden-retry")))
			}))
			defer fallback.Close()

			db, engine := newSettlementDispatchIntegrationHarness(t, fmt.Sprintf("stream-uncertain-%d", index), primary.URL, constant.ChannelTypeOpenAI, "")
			addSettlementFallbackChannel(t, db, fallback.URL)
			if testCase.mode == "log_persistence_failure" {
				require.NoError(t, db.Exec(`CREATE TRIGGER settlement_test_log_failure
					BEFORE INSERT ON logs
					WHEN NEW.settlement_binding_id IS NOT NULL
					BEGIN
						SELECT RAISE(ABORT, 'injected linked log failure');
					END`).Error)
			}

			originalRetries := common.RetryTimes
			common.RetryTimes = 1
			t.Cleanup(func() { common.RetryTimes = originalRetries })

			requestID := settlementDispatchOpaque(byte(index + 180))
			nonce := settlementDispatchOpaque(byte(index + 190))
			response := performSettlementDispatchRequestWithContext(
				inboundContext,
				engine,
				http.MethodPost,
				"/v1/chat/completions",
				`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`,
				requestID,
				nonce,
			)
			assert.Equal(t, http.StatusOK, response.Code)
			assert.EqualValues(t, 1, primaryWires.Load())
			assert.Zero(t, fallbackWires.Load())

			var binding model.SettlementReadbackBinding
			require.NoError(t, db.Where("dispatch_token_id = ?", 42).First(&binding).Error)
			assert.Equal(t, model.SettlementReadbackBindingDispatchStarted, binding.State)
			var dispatchCAS int64
			require.NoError(t, db.Table("settlement_dispatch_audit").Count(&dispatchCAS).Error)
			assert.EqualValues(t, 1, dispatchCAS)
			var linkedLogs int64
			require.NoError(t, db.Model(&model.Log{}).Where("settlement_binding_id = ?", binding.Id).Count(&linkedLogs).Error)
			assert.Zero(t, linkedLogs)

			replay := performSettlementDispatchRequest(engine, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`, requestID, nonce)
			assert.Equal(t, http.StatusConflict, replay.Code)
			assert.EqualValues(t, 1, primaryWires.Load())
			assert.Zero(t, fallbackWires.Load())
		})
	}
}

func addSettlementFallbackChannel(t *testing.T, db *gorm.DB, fallbackURL string) {
	t.Helper()
	priority := int64(0)
	require.NoError(t, db.Create(&model.Channel{
		Id:       8,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "local-fallback-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "local-fallback",
		BaseURL:  &fallbackURL,
		Models:   "gpt-4o-mini",
		Group:    "default",
		Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4o-mini", ChannelId: 8, Enabled: true, Priority: &priority}).Error)
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
	originalStreamingTimeout := constant.StreamingTimeout
	common.SQLitePath = filepath.Join(t.TempDir(), "settlement-router-"+dsnName+".db") + "?_busy_timeout=30000&_pragma=foreign_keys(1)"
	common.IsMasterNode = false
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}))
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
	constant.StreamingTimeout = 30
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
		constant.StreamingTimeout = originalStreamingTimeout
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
	primaryPriority := int64(10)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7).Update("priority", primaryPriority).Error)
	for _, modelName := range []string{"gpt-4o-mini", "claude-3-haiku-20240307"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: 7, Enabled: true, Priority: &primaryPriority}).Error)
	}

	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	SetVideoRouter(engine)
	return db, engine
}

func performSettlementDispatchRequest(engine http.Handler, method string, target string, body string, requestID string, nonce string) *httptest.ResponseRecorder {
	return performSettlementDispatchRequestWithAuthorization(engine, method, target, body, "Bearer sk-settlementdispatch", requestID, nonce)
}

func performSettlementDispatchRequestWithContext(requestContext context.Context, engine http.Handler, method string, target string, body string, requestID string, nonce string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body)).WithContext(requestContext)
	request.Header.Set("Authorization", "Bearer sk-settlementdispatch")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(common.SettlementReadbackRequestIdHeader, requestID)
	request.Header.Set(common.SettlementReadbackNonceHeader, nonce)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func performSettlementDispatchRequestWithAuthorization(engine http.Handler, method string, target string, body string, authorization string, requestID string, nonce string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		request.Header.Set(common.SettlementReadbackRequestIdHeader, requestID)
	}
	if nonce != "" {
		request.Header.Set(common.SettlementReadbackNonceHeader, nonce)
	}
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

func settlementChatStreamChunk(id string) string {
	return `data: {"id":"` + id + `","object":"chat.completion.chunk","created":1710000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + "\n\n"
}

func settlementChatStreamBody(id string) string {
	return settlementChatStreamChunk(id) +
		`data: {"id":"` + id + `","object":"chat.completion.chunk","created":1710000000,"model":"gpt-4o-mini","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" +
		"data: [DONE]\n\n"
}

func settlementResponsesStreamBody(id string) string {
	return `data: {"type":"response.output_text.delta","delta":"ok"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"` + id + `","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}}` + "\n\n" +
		"data: [DONE]\n\n"
}

func settlementClaudeStreamBody() string {
	return strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg-stream","type":"message","role":"assistant","model":"claude-3-haiku-20240307","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
}
