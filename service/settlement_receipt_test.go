package service

import (
	"context"
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

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
