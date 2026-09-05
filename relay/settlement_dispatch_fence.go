package relay

import (
	"errors"
	"net/http"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func settlementDispatchFenceUnavailable() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		errors.New("settlement dispatch fence unavailable"),
		types.ErrorCodeSettlementDispatchFenceUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func requireSettlementDispatchFence(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor) *types.NewAPIError {
	if info == nil || info.SettlementBindingId == 0 {
		return nil
	}
	info.SettlementDispatchFence = ""
	expectedAPIType, explicit := rootcommon.ChannelType2APIType(info.ChannelType)
	if !explicit || expectedAPIType != info.ApiType || adaptor == nil {
		return settlementDispatchFenceUnavailable()
	}
	profile, allowed := channel.ResolveSettlementDispatchFence(adaptor, c, info)
	if !allowed {
		return settlementDispatchFenceUnavailable()
	}
	info.SettlementDispatchFence = string(profile)
	return nil
}

func ValidateSettlementDispatchPreflight(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if info == nil || info.SettlementBindingId == 0 {
		return nil
	}
	probe := *info
	probe.Request = nil
	probe.ChannelMeta = nil
	probe.SettlementDispatchFence = ""
	probe.InitChannelMeta(c)
	service.FreezeChatCompletionsDispatchPlan(&probe)
	adaptor := GetAdaptor(probe.ApiType)
	if fenceError := requireSettlementDispatchFence(c, &probe, adaptor); fenceError != nil {
		return fenceError
	}
	if info.Request == nil {
		return settlementDispatchFenceUnavailable()
	}
	meta := info.Request.GetTokenCountMeta()
	if meta == nil {
		return nil
	}
	for _, file := range meta.Files {
		if file != nil && file.IsURL() {
			return settlementDispatchFenceUnavailable()
		}
	}
	return nil
}
