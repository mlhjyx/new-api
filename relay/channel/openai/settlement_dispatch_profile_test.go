package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSettlementDispatchFenceMatchesActualPassThroughPlan(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalPassThrough := settings.PassThroughRequestEnabled
	originalPolicy := settings.ChatCompletionsToResponsesPolicy
	t.Cleanup(func() {
		settings.PassThroughRequestEnabled = originalPassThrough
		settings.ChatCompletionsToResponsesPolicy = originalPolicy
	})
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-4o-mini$"},
	}

	tests := []struct {
		name               string
		globalPassThrough  bool
		channelPassThrough bool
		wantPlan           service.ChatCompletionsDispatchPlan
		want               channel.SettlementDispatchFence
	}{
		{name: "conversion enabled", wantPlan: service.ChatCompletionsDispatchResponses, want: channel.SettlementDispatchFenceOpenAIChatViaResponses},
		{name: "global pass-through forces direct", globalPassThrough: true, wantPlan: service.ChatCompletionsDispatchDirect, want: channel.SettlementDispatchFenceOpenAIChat},
		{name: "channel pass-through forces direct", channelPassThrough: true, wantPlan: service.ChatCompletionsDispatchDirect, want: channel.SettlementDispatchFenceOpenAIChat},
		{name: "both pass-through flags force direct", globalPassThrough: true, channelPassThrough: true, wantPlan: service.ChatCompletionsDispatchDirect, want: channel.SettlementDispatchFenceOpenAIChat},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = testCase.globalPassThrough
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeChatCompletions,
				OriginModelName: "gpt-4o-mini",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeOpenAI,
					ChannelId:   7,
					ApiType:     constant.APITypeOpenAI,
					ChannelSetting: dto.ChannelSettings{
						PassThroughBodyEnabled: testCase.channelPassThrough,
					},
				},
			}

			assert.Equal(t, testCase.wantPlan, service.FreezeChatCompletionsDispatchPlan(info))
			assert.Equal(t, testCase.want, (&Adaptor{}).SettlementDispatchFence(context, info))
		})
	}
}
