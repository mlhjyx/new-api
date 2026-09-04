package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func requireSettlementDispatchFence(info *relaycommon.RelayInfo, adaptor channel.Adaptor) *types.NewAPIError {
	if info == nil || info.SettlementBindingId == 0 {
		return nil
	}
	if adaptor != nil && channel.HasSettlementDispatchFence(adaptor) {
		return nil
	}
	return types.NewErrorWithStatusCode(
		errors.New("settlement dispatch fence unavailable"),
		types.ErrorCodeSettlementDispatchFenceUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}
