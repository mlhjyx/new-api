package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// maxStreamErrorFieldLength bounds the upstream error type/code copied into
// logs and relay errors; upstream messages are never copied.
const maxStreamErrorFieldLength = 64

// upstreamStreamErrorEvent is the bounded, log-safe identity of an upstream
// `data: {"error": ...}` event inside an OpenAI-compatible stream. Type and Code
// are empty when the upstream did not send a usable scalar value.
type upstreamStreamErrorEvent struct {
	Type string
	Code string
}

func parseUpstreamStreamErrorEvent(data string) (upstreamStreamErrorEvent, bool) {
	// Cheap pre-filter: ordinary content chunks never carry an "error" key.
	if !strings.Contains(data, `"error"`) {
		return upstreamStreamErrorEvent{}, false
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &envelope); err != nil {
		return upstreamStreamErrorEvent{}, false
	}
	switch common.GetJsonType(envelope.Error) {
	case "unknown", "null":
		return upstreamStreamErrorEvent{}, false
	case "object":
		var fields struct {
			Type any `json:"type"`
			Code any `json:"code"`
		}
		if err := common.Unmarshal(envelope.Error, &fields); err != nil {
			return upstreamStreamErrorEvent{}, true
		}
		return upstreamStreamErrorEvent{
			Type: sanitizeStreamErrorField(fields.Type),
			Code: sanitizeStreamErrorField(fields.Code),
		}, true
	default:
		return upstreamStreamErrorEvent{}, true
	}
}

// sanitizeStreamErrorField keeps only short scalar identifiers made of
// [A-Za-z0-9_.:-]; anything else is replaced so model text cannot leak.
func sanitizeStreamErrorField(value any) string {
	var raw string
	switch typed := value.(type) {
	case string:
		raw = typed
	case float64:
		raw = strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
	if len(raw) > maxStreamErrorFieldLength {
		raw = raw[:maxStreamErrorFieldLength]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '_' || r == '.' || r == ':' || r == '-':
			return r
		default:
			return '_'
		}
	}, raw)
}

// checkUpstreamStreamEnd classifies how an OpenAI-compatible upstream stream
// ended once the scanner has stopped and before anything else is written.
//
// A stream is abnormal when it carried an upstream error event, or when it
// stopped without a terminal event: no [DONE] and no final chunk with a
// finish_reason or usage. Abnormal ends are logged at ERROR level. If nothing
// has reached the client yet (OaiStreamHandler forwards a chunk only once the
// next one arrives, and keepalive pings mark the writer as written), the
// failure is returned as a retryable 502 so the relay retry loop can try again
// and no consume log is written for this attempt. Settlement-bound requests
// keep the fork's single-dispatch semantics and are never turned into a retry
// here; service.SettlementStreamCompletionError still rejects their receipt.
func checkUpstreamStreamEnd(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, lastStreamData string, receivedChunks int, errorEvent *upstreamStreamErrorEvent) *types.NewAPIError {
	endReason := relaycommon.StreamEndReasonNone
	if info.StreamStatus != nil {
		endReason = info.StreamStatus.EndReason
	}
	reachedTerminal := endReason == relaycommon.StreamEndReasonDone
	if !reachedTerminal && lastStreamData != "" {
		var lastChunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(lastStreamData, &lastChunk); err == nil {
			reachedTerminal = lastChunk.IsFinished() || service.ValidUsage(lastChunk.Usage)
		}
	}
	if errorEvent == nil && reachedTerminal {
		return nil
	}

	written := c.Writer.Written()
	retryable := info.SettlementBindingId == 0 && info.SendResponseCount == 0 && !written
	switch endReason {
	case relaycommon.StreamEndReasonEOF, relaycommon.StreamEndReasonScannerErr,
		relaycommon.StreamEndReasonTimeout, relaycommon.StreamEndReasonDone:
	default:
		// client_gone, panic, ping_fail: another attempt cannot help this client.
		retryable = false
	}

	errorType, errorCode := "-", "-"
	if errorEvent != nil && errorEvent.Type != "" {
		errorType = errorEvent.Type
	}
	if errorEvent != nil && errorEvent.Code != "" {
		errorCode = errorEvent.Code
	}
	proto := "-"
	if resp != nil && resp.Proto != "" {
		proto = resp.Proto
	}
	logger.LogError(c, fmt.Sprintf(
		"stream ended abnormally: channel_id=%d reason=%s received=%d sent=%d written=%t upstream_error_type=%s upstream_error_code=%s proto=%s retry_before_first_byte=%t",
		info.ChannelId, endReason, receivedChunks, info.SendResponseCount, written, errorType, errorCode, proto, retryable,
	))
	if !retryable {
		return nil
	}
	return types.NewOpenAIError(
		fmt.Errorf("upstream stream ended before the first response byte (reason=%s, upstream_error_code=%s)", endReason, errorCode),
		types.ErrorCodeUpstreamStreamAbortedBeforeFirstByte,
		http.StatusBadGateway,
	)
}
