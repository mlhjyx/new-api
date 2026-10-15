package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type settlementUpstreamMetadataBilling struct{}

func (settlementUpstreamMetadataBilling) Settle(int) error         { return nil }
func (settlementUpstreamMetadataBilling) Refund(*gin.Context)      {}
func (settlementUpstreamMetadataBilling) NeedsRefund() bool        { return false }
func (settlementUpstreamMetadataBilling) GetPreConsumedQuota() int { return 0 }
func (settlementUpstreamMetadataBilling) Reserve(int) error        { return nil }

// Since upstream v1.0.0-rc.44 a consume log stores its content as
// other.content_parts and may add response_model, tool_surcharges and tiered
// billing projections at the top level of other. The exact receipt must still
// close over these logs, while any other unknown key keeps failing closed.
func TestSettlementReadbackClosedReceiptAcceptsUpstreamConsumeLogMetadata(t *testing.T) {
	baseLog := model.Log{
		Type:             model.LogTypeConsume,
		ModelName:        "gpt-4o-mini",
		ChannelId:        7,
		Quota:            1250,
		PromptTokens:     10,
		CompletionTokens: 20,
	}
	upstreamKeys := `"content_parts":[{"key":"Upstream returned no usage"}],` +
		`"response_model":{"requested_model":"gpt-4o-mini","upstream_model":"","returned_model":"gpt-4o-mini-2024-07-18"},` +
		`"tool_surcharges":[{"name":"web_search","count":1,"price":10}],` +
		`"billing_mode":"tiered_expr","expr_b64":"","matched_tier":"t1","billing_unit":"token",` +
		`"billing_tokens":{"p":10,"c":20},"image_cache_tokens":0,"image_count":1,"fixed_price":0.5,"request_rules":[]`

	accepted := baseLog
	accepted.Other = `{"usage_semantic":"openai","cache_creation_tokens":0,"cache_tokens":2,` + upstreamKeys + `}`
	receipt, ok := settlementReadbackClosedReceipt(strings.Repeat("C", 43), &accepted)
	require.True(t, ok)
	assert.Equal(t, "openai", receipt.UsageSemantic)
	assert.EqualValues(t, 2, receipt.CacheReadTokens)

	rejected := baseLog
	rejected.Other = `{"usage_semantic":"openai","cache_creation_tokens":0,"cache_tokens":2,` + upstreamKeys + `,"root_info":{"node":"x"}}`
	_, ok = settlementReadbackClosedReceipt(strings.Repeat("C", 43), &rejected)
	assert.False(t, ok, "keys outside the text consume path stay outside the closed receipt")
}

// The receipt is produced by the real upstream consume-log writer, not by a
// hand-written fixture, so a future upstream key surfaces here first.
func TestSettlementReadbackClosedReceiptCoversRealConsumeLogWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-upstream-writer?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(db))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}))
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalConsume, originalExport, originalBatch := common.LogConsumeEnabled, common.DataExportEnabled, common.BatchUpdateEnabled
	originalRedis := common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	common.LogConsumeEnabled, common.DataExportEnabled, common.BatchUpdateEnabled = true, false, false
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = originalRedis
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetMainDatabaseType(originalMainType)
		common.SetLogDatabaseType(originalLogType)
		common.LogConsumeEnabled, common.DataExportEnabled, common.BatchUpdateEnabled = originalConsume, originalExport, originalBatch
	})

	for index, testCase := range []struct {
		name         string
		extraContent []*common.Message
		usage        *dto.Usage
	}{
		{name: "content parts and response model", extraContent: []*common.Message{common.NewMessage("upstream note")}, usage: &dto.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		{name: "missing usage note", usage: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			binding := &model.SettlementReadbackBinding{
				DispatchTokenId:           4200 + index,
				SettlementRequestIdSha256: strings.Repeat(string(rune('a'+index)), 64),
				SettlementNonceSha256:     strings.Repeat(string(rune('c'+index)), 64),
				GatewayRequestId:          "gateway-upstream-writer",
				State:                     model.SettlementReadbackBindingDispatchStarted,
				CreatedAt:                 time.Now().Unix(),
			}
			require.NoError(t, db.Create(binding).Error)
			relayInfo := &relaycommon.RelayInfo{
				UserId:              4100 + index,
				TokenId:             binding.DispatchTokenId,
				OriginModelName:     "gpt-4o-mini",
				SettlementBindingId: binding.Id,
				StartTime:           time.Now(),
				Billing:             settlementUpstreamMetadataBilling{},
				ChannelMeta:         &relaycommon.ChannelMeta{ChannelId: 4300 + index, UpstreamModelName: "gpt-4o-mini"},
				ResponseModel:       &relaycommon.ResponseModel{RequestedModel: "gpt-4o-mini", ReturnedModel: "gpt-4o-mini-2024-07-18"},
				PriceData: hosttypes.PriceData{
					ModelRatio:      1,
					CompletionRatio: 1,
					GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
				},
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			require.Nil(t, service.PostTextConsumeQuota(ctx, relayInfo, testCase.usage, testCase.extraContent))

			var log model.Log
			require.NoError(t, db.Where("settlement_binding_id = ?", binding.Id).First(&log).Error)
			_, ok := settlementReadbackClosedReceipt(strings.Repeat("D", 43), &log)
			assert.True(t, ok, "closed receipt rejected upstream consume log metadata: %s", log.Other)
		})
	}
}
