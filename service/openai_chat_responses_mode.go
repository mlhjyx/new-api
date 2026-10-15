package service

import (
	"regexp"
	"sync"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

type ChatCompletionsDispatchPlan string

const (
	ChatCompletionsDispatchDirect    ChatCompletionsDispatchPlan = "direct"
	ChatCompletionsDispatchResponses ChatCompletionsDispatchPlan = "openai_responses"
)

// Chat→Responses upgrade policy is host routing logic (it decides *whether*
// to convert, reading host settings), so it lives here, not in relayconvert.

var chatResponsesRegexCache sync.Map // map[string]*regexp.Regexp

func matchAnyModelPattern(patterns []string, model string) bool {
	if len(patterns) == 0 || model == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := chatResponsesRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				// Treat invalid patterns as non-matching to avoid breaking runtime traffic.
				continue
			}
			re = compiled
			chatResponsesRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(model) {
			return true
		}
	}
	return false
}

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	if !policy.IsChannelEnabled(channelID, channelType) {
		return false
	}
	return matchAnyModelPattern(policy.ModelPatterns, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		channelID,
		channelType,
		model,
	)
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
