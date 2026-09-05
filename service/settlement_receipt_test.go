package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type settlementTestBilling struct {
	settleCalls int
	settleErr   error
}

func (billing *settlementTestBilling) Settle(int) error {
	billing.settleCalls++
	return billing.settleErr
}

func (*settlementTestBilling) Refund(*gin.Context)      {}
func (*settlementTestBilling) NeedsRefund() bool        { return true }
func (*settlementTestBilling) GetPreConsumedQuota() int { return 15 }
func (*settlementTestBilling) Reserve(int) error        { return nil }

func settlementReceiptBinding(t *testing.T, tokenID int, requestByte string, nonceByte string) *model.SettlementReadbackBinding {
	t.Helper()
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(model.DB))
	binding := &model.SettlementReadbackBinding{
		DispatchTokenId:           tokenID,
		SettlementRequestIdSha256: strings.Repeat(requestByte, 64),
		SettlementNonceSha256:     strings.Repeat(nonceByte, 64),
		GatewayRequestId:          "gateway-test",
		State:                     model.SettlementReadbackBindingDispatchStarted,
		CreatedAt:                 time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(binding).Error)
	t.Cleanup(func() {
		model.DB.Where("settlement_binding_id = ?", binding.Id).Delete(&model.Log{})
		model.DB.Delete(binding)
	})
	return binding
}

func TestSettlementReceiptPersistenceErrorIsPostWireAndNeverRetryable(t *testing.T) {
	err := SettlementReceiptPersistenceError()
	assert.Equal(t, types.ErrorCodeSettlementPersistenceFailed, err.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
	assert.NotContains(t, err.Error(), "sql")
	assert.NotContains(t, err.Error(), "credential")
}

func TestSettlementStreamCompletionErrorRequiresVerifiedTerminalEventAndNeverRetries(t *testing.T) {
	for _, status := range []*relaycommon.StreamStatus{
		nil,
		func() *relaycommon.StreamStatus {
			value := relaycommon.NewStreamStatus()
			value.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
			return value
		}(),
		func() *relaycommon.StreamStatus {
			value := relaycommon.NewStreamStatus()
			value.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
			return value
		}(),
		func() *relaycommon.StreamStatus {
			value := relaycommon.NewStreamStatus()
			value.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			value.RecordError("malformed terminal chunk")
			return value
		}(),
	} {
		err := SettlementStreamCompletionError(&relaycommon.RelayInfo{SettlementBindingId: 1, IsStream: true, StreamStatus: status})
		assert.Equal(t, types.ErrorCodeSettlementStreamIncomplete, err.GetErrorCode())
		assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
		assert.True(t, types.IsSkipRetryError(err))
		assert.NotContains(t, err.Error(), "context")
		assert.NotContains(t, err.Error(), "malformed")
	}

	complete := relaycommon.NewStreamStatus()
	complete.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	assert.Nil(t, SettlementStreamCompletionError(&relaycommon.RelayInfo{SettlementBindingId: 1, IsStream: true, StreamStatus: complete}))
	protocolTerminal := relaycommon.NewStreamStatus()
	protocolTerminal.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	protocolTerminal.MarkTerminalEventObserved()
	assert.Nil(t, SettlementStreamCompletionError(&relaycommon.RelayInfo{SettlementBindingId: 1, IsStream: true, StreamStatus: protocolTerminal}))
	assert.Nil(t, SettlementStreamCompletionError(&relaycommon.RelayInfo{SettlementBindingId: 0, IsStream: true}))
}

func TestPostTextConsumeQuotaDoesNotLinkReceiptWhenBillingSettlementFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() { common.DataExportEnabled = previousExport })

	binding := settlementReceiptBinding(t, 424242, "a", "b")

	billing := &settlementTestBilling{settleErr: errors.New("funding adjustment failed")}
	relayInfo := &relaycommon.RelayInfo{
		UserId:              414141,
		TokenId:             binding.DispatchTokenId,
		OriginModelName:     "gpt-4o-mini",
		SettlementBindingId: binding.Id,
		StartTime:           time.Now(),
		Billing:             billing,
		ChannelMeta:         &relaycommon.ChannelMeta{ChannelId: 434343},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	settlementErr := PostTextConsumeQuota(ctx, relayInfo, &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}, nil)

	assert.Nil(t, settlementErr, "the already-written valid output must remain successful")
	assert.Equal(t, 1, billing.settleCalls)
	var stored model.SettlementReadbackBinding
	require.NoError(t, model.DB.First(&stored, binding.Id).Error)
	assert.Equal(t, model.SettlementReadbackBindingDispatchStarted, stored.State)
	var linkedLogs int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("settlement_binding_id = ?", binding.Id).Count(&linkedLogs).Error)
	assert.Zero(t, linkedLogs)
	linkedLog, pending, err := model.FindSettlementReadbackConsumeLog(
		model.DB,
		binding.DispatchTokenId,
		binding.SettlementRequestIdSha256,
		binding.SettlementNonceSha256,
	)
	require.NoError(t, err)
	assert.True(t, pending)
	assert.Nil(t, linkedLog)
}

func TestPostTextConsumeQuotaKeepsBoundGizmoModelExact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousExport := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() { common.DataExportEnabled = previousExport })

	for index, modelName := range []string{"gpt-4-gizmo-custom", "gpt-4o-gizmo-custom"} {
		t.Run(modelName, func(t *testing.T) {
			binding := settlementReceiptBinding(t, 424243+index, string(rune('c'+index)), string(rune('e'+index)))
			billing := &settlementTestBilling{}
			relayInfo := &relaycommon.RelayInfo{
				UserId:              414142 + index,
				TokenId:             binding.DispatchTokenId,
				OriginModelName:     modelName,
				SettlementBindingId: binding.Id,
				StartTime:           time.Now(),
				Billing:             billing,
				ChannelMeta:         &relaycommon.ChannelMeta{ChannelId: 434344 + index},
				PriceData: types.PriceData{
					ModelRatio:      0,
					CompletionRatio: 1,
					GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
				},
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

			settlementErr := PostTextConsumeQuota(ctx, relayInfo, &dto.Usage{
				PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15,
			}, nil)

			require.Nil(t, settlementErr)
			var log model.Log
			require.NoError(t, model.DB.Where("settlement_binding_id = ?", binding.Id).First(&log).Error)
			assert.Equal(t, modelName, log.ModelName)
			assert.NotContains(t, log.ModelName, "*")
			var stored model.SettlementReadbackBinding
			require.NoError(t, model.DB.First(&stored, binding.Id).Error)
			assert.Equal(t, model.SettlementReadbackBindingLogLinked, stored.State)
		})
	}
}
