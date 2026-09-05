package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

type ChatCompletionsDispatchPlan string

const (
	ChatCompletionsDispatchDirect    ChatCompletionsDispatchPlan = "direct"
	ChatCompletionsDispatchResponses ChatCompletionsDispatchPlan = "openai_responses"
)

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	return relayconvert.ShouldChatCompletionsUseResponsesPolicy(policy, channelID, channelType, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return relayconvert.ShouldChatCompletionsUseResponsesGlobal(channelID, channelType, model)
}

func FreezeChatCompletionsDispatchPlan(info *relaycommon.RelayInfo) ChatCompletionsDispatchPlan {
	plan := ChatCompletionsDispatchDirect
	if info != nil &&
		!model_setting.GetGlobalSettings().PassThroughRequestEnabled &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		plan = ChatCompletionsDispatchResponses
	}
	if info != nil {
		info.ChatCompletionsDispatchPlan = string(plan)
	}
	return plan
}

func FrozenChatCompletionsDispatchPlan(info *relaycommon.RelayInfo) (ChatCompletionsDispatchPlan, bool) {
	if info == nil {
		return "", false
	}
	switch ChatCompletionsDispatchPlan(info.ChatCompletionsDispatchPlan) {
	case ChatCompletionsDispatchDirect:
		return ChatCompletionsDispatchDirect, true
	case ChatCompletionsDispatchResponses:
		return ChatCompletionsDispatchResponses, true
	default:
		return "", false
	}
}
