package channel

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor interface {
	// Init IsStream bool
	Init(info *relaycommon.RelayInfo)
	GetRequestURL(info *relaycommon.RelayInfo) (string, error)
	SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error
	ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error)
	ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error)
	ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error)
	ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error)
	ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error)
	ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error)
	DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error)
	DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError)
	GetModelList() []string
	GetChannelName() string
	ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error)
	ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error)
}

type SettlementDispatchFence string

const (
	SettlementDispatchFenceOpenAIChat                     SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=chat/stream=false/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceOpenAIChatStream               SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=chat/stream=true/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceOpenAIResponses                SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=responses/stream=false/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceOpenAIResponsesStream          SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=responses/stream=true/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceOpenAIChatViaResponses         SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=chat/stream=false/converter=openai_chat_completions_to_openai_responses/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceOpenAIChatViaResponsesStream   SettlementDispatchFence = "settlement-dispatch/v1/channel=openai/api=openai/variant=chat/stream=true/converter=openai_chat_completions_to_openai_responses/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceAnthropicMessages              SettlementDispatchFence = "settlement-dispatch/v1/channel=anthropic/api=anthropic/variant=messages/stream=false/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceAnthropicMessagesStream        SettlementDispatchFence = "settlement-dispatch/v1/channel=anthropic/api=anthropic/variant=messages/stream=true/converter=none/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceAdvancedResponsesViaChat       SettlementDispatchFence = "settlement-dispatch/v1/channel=advanced-custom/api=advanced-custom/variant=responses/stream=false/converter=openai_responses_to_openai_chat_completions/primitive=common-http-no-redirect-no-aux/v1"
	SettlementDispatchFenceAdvancedResponsesViaChatStream SettlementDispatchFence = "settlement-dispatch/v1/channel=advanced-custom/api=advanced-custom/variant=responses/stream=true/converter=openai_responses_to_openai_chat_completions/primitive=common-http-no-redirect-no-aux/v1"
)

type SettlementDispatchFencedAdaptor interface {
	SettlementDispatchFence(c *gin.Context, info *relaycommon.RelayInfo) SettlementDispatchFence
}

func ResolveSettlementDispatchFence(adaptor Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (SettlementDispatchFence, bool) {
	fenced, ok := adaptor.(SettlementDispatchFencedAdaptor)
	if !ok {
		return "", false
	}
	profile := fenced.SettlementDispatchFence(c, info)
	switch profile {
	case SettlementDispatchFenceOpenAIChat,
		SettlementDispatchFenceOpenAIChatStream,
		SettlementDispatchFenceOpenAIResponses,
		SettlementDispatchFenceOpenAIResponsesStream,
		SettlementDispatchFenceOpenAIChatViaResponses,
		SettlementDispatchFenceOpenAIChatViaResponsesStream,
		SettlementDispatchFenceAnthropicMessages,
		SettlementDispatchFenceAnthropicMessagesStream,
		SettlementDispatchFenceAdvancedResponsesViaChat,
		SettlementDispatchFenceAdvancedResponsesViaChatStream:
		return profile, true
	default:
		return "", false
	}
}

type TaskAdaptor interface {
	Init(info *relaycommon.RelayInfo)

	ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError

	// ── Billing ──────────────────────────────────────────────────────

	// EstimateBilling returns OtherRatios for pre-charge based on user request.
	// Called after ValidateRequestAndSetAction, before price calculation.
	// Adaptors should extract duration, resolution, etc. from the parsed request
	// and return them as ratio multipliers (e.g. {"seconds": 5, "size": 1.666}).
	// Return nil to use the base model price without extra ratios.
	EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64

	// AdjustBillingOnSubmit returns adjusted OtherRatios from the upstream
	// submit response. Called after a successful DoResponse.
	// If the upstream returned actual parameters that differ from the estimate
	// (e.g. actual seconds), return updated ratios so the caller can recalculate
	// the quota and settle the delta with the pre-charge.
	// Return nil if no adjustment is needed.
	AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64

	// AdjustBillingOnComplete returns the actual quota when a task reaches a
	// terminal state (success/failure) during polling.
	// Called by the polling loop after ParseTaskResult.
	// Return a positive value to trigger delta settlement (supplement / refund).
	// Return 0 to keep the pre-charged amount unchanged.
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int

	// ── Request / Response ───────────────────────────────────────────

	BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
	BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
	BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)

	DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error)
	DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, err *dto.TaskError)

	GetModelList() []string
	GetChannelName() string

	// ── Polling ──────────────────────────────────────────────────────

	FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error)
}

type OpenAIVideoConverter interface {
	ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error)
}
