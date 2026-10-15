package openai

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	firstByteRoleChunk     = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`
	firstByteContentChunk  = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`
	firstByteFinishChunk   = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`
	firstByteUsageChunk    = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-v4-pro","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
	firstByteErrorEvent    = `{"error":{"message":"private prompt echo must not be logged","type":"server_error","code":"upstream_timeout"}}`
	firstByteRequestID     = "first-byte-request"
	firstByteChannelID     = 24
	firstBytePrivateEchoed = "private prompt echo"
)

func sseEvents(events ...string) string {
	var b strings.Builder
	for _, event := range events {
		b.WriteString("data: ")
		b.WriteString(event)
		b.WriteString("\n\n")
	}
	return b.String()
}

type firstByteStreamCase struct {
	name string
	// upstream writes the SSE body; returning ends the response with a clean EOF.
	upstream          func(t *testing.T, w http.ResponseWriter, r *http.Request)
	settlementBinding int
	pingBeforeStream  bool
	streamingTimeout  int

	wantRetryableError bool
	wantBodyContains   []string
	wantAbnormalLog    []string
	wantNoAbnormalLog  bool
}

func writeSSE(body string) func(t *testing.T, w http.ResponseWriter, r *http.Request) {
	return func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

// abortChunkedBody sends the response headers with chunked framing and then
// drops the connection before the terminating chunk, which the client sees as
// io.ErrUnexpectedEOF (a scanner_error in the stream scanner).
func abortChunkedBody(t *testing.T, w http.ResponseWriter, _ *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Error("upstream writer cannot be hijacked")
		return
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		t.Errorf("hijack upstream connection: %v", err)
		return
	}
	_, _ = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
	_ = buffer.Flush()
	_ = conn.Close()
}

// stallAfterHeaders sends the headers and then waits until the relay gives up
// on the stream and closes the connection.
func stallAfterHeaders(t *testing.T, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	<-r.Context().Done()
}

func TestOaiStreamHandlerFirstByteFailures(t *testing.T) {
	tests := []firstByteStreamCase{
		{
			name:               "eof before any chunk is retryable",
			upstream:           writeSSE(""),
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=eof", "received=0", "written=false"},
		},
		{
			name:               "error event then close is retryable",
			upstream:           writeSSE(sseEvents(firstByteErrorEvent)),
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=eof", "received=1", "written=false", "upstream_error_type=server_error", "upstream_error_code=upstream_timeout"},
		},
		{
			name:               "error event followed by done is retryable",
			upstream:           writeSSE(sseEvents(firstByteErrorEvent, "[DONE]")),
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=done", "received=1", "written=false", "upstream_error_code=upstream_timeout"},
		},
		{
			name:               "one content chunk then eof is retryable because the first chunk is still buffered",
			upstream:           writeSSE(sseEvents(firstByteRoleChunk)),
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=eof", "received=1", "written=false", "upstream_error_type=- upstream_error_code=-"},
		},
		{
			name:               "scanner error before any chunk is retryable",
			upstream:           abortChunkedBody,
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=scanner_error", "received=0", "written=false"},
		},
		{
			name:               "streaming timeout before any chunk is retryable",
			upstream:           stallAfterHeaders,
			streamingTimeout:   1,
			wantRetryableError: true,
			wantAbnormalLog:    []string{"reason=timeout", "received=0", "written=false"},
		},
		{
			name:              "full normal stream is relayed unchanged",
			upstream:          writeSSE(sseEvents(firstByteRoleChunk, firstByteFinishChunk, firstByteUsageChunk, "[DONE]")),
			wantBodyContains:  []string{`"content":"Hel"`, `"finish_reason":"stop"`, "data: [DONE]"},
			wantNoAbnormalLog: true,
		},
		{
			name:              "single terminal chunk without done is a complete response",
			upstream:          writeSSE(sseEvents(firstByteFinishChunk)),
			wantBodyContains:  []string{`"finish_reason":"stop"`, "data: [DONE]"},
			wantNoAbnormalLog: true,
		},
		{
			name:             "eof after the first chunk was written keeps relaying and logs the abnormal end",
			upstream:         writeSSE(sseEvents(firstByteRoleChunk, firstByteContentChunk)),
			wantBodyContains: []string{`"content":"Hel"`, `"content":"lo"`, "data: [DONE]"},
			wantAbnormalLog:  []string{"reason=eof", "received=2", "sent=1", "written=true"},
		},
		{
			name:             "error event after the first chunk was written keeps relaying and logs its code",
			upstream:         writeSSE(sseEvents(firstByteRoleChunk, firstByteErrorEvent)),
			wantBodyContains: []string{`"content":"Hel"`, `"code":"upstream_timeout"`},
			wantAbnormalLog:  []string{"reason=eof", "received=2", "written=true", "upstream_error_type=server_error", "upstream_error_code=upstream_timeout"},
		},
		{
			name:             "keepalive ping already written keeps today's behaviour",
			upstream:         writeSSE(""),
			pingBeforeStream: true,
			wantBodyContains: []string{": PING", "data: [DONE]"},
			wantAbnormalLog:  []string{"reason=eof", "received=0", "written=true"},
		},
		{
			name:              "settlement-bound eof before any chunk keeps today's behaviour",
			upstream:          writeSSE(""),
			settlementBinding: 7,
			wantBodyContains:  []string{"data: [DONE]"},
			wantAbnormalLog:   []string{"reason=eof", "received=0"},
		},
		{
			name:              "settlement-bound error event keeps today's behaviour",
			upstream:          writeSSE(sseEvents(firstByteErrorEvent)),
			settlementBinding: 7,
			wantBodyContains:  []string{`"code":"upstream_timeout"`, "data: [DONE]"},
			wantAbnormalLog:   []string{"upstream_error_code=upstream_timeout"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			originalTimeout := constant.StreamingTimeout
			constant.StreamingTimeout = 30
			if tc.streamingTimeout > 0 {
				constant.StreamingTimeout = tc.streamingTimeout
			}
			t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
			logs := captureRelayLogs(t)
			resp := openFirstByteUpstream(t, tc.upstream)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(common.RequestIdKey, firstByteRequestID)
			info := newFirstByteRelayInfo(tc.settlementBinding)
			if tc.pingBeforeStream {
				_, err := c.Writer.WriteString(": PING\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
			}

			// Act
			usage, apiErr := OaiStreamHandler(c, info, resp)

			// Assert
			if tc.wantRetryableError {
				require.NotNil(t, apiErr)
				assert.Nil(t, usage)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Equal(t, types.ErrorCodeUpstreamStreamAbortedBeforeFirstByte, apiErr.GetErrorCode())
				assert.False(t, types.IsSkipRetryError(apiErr))
				assert.False(t, c.Writer.Written(), "nothing may reach the client before the retry")
				assert.Zero(t, recorder.Body.Len())
				assert.Zero(t, info.SendResponseCount)
				assert.NotContains(t, apiErr.Error(), firstBytePrivateEchoed)
			} else {
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				for _, want := range tc.wantBodyContains {
					assert.Contains(t, recorder.Body.String(), want)
				}
			}

			logText := logs.String()
			assert.NotContains(t, logText, firstBytePrivateEchoed, "upstream error messages must never be logged")
			if tc.wantNoAbnormalLog {
				assert.NotContains(t, logText, "stream ended abnormally")
				return
			}
			abnormalLine := findLogLine(logText, "stream ended abnormally")
			require.NotEmpty(t, abnormalLine, "abnormal stream end must be logged at ERROR level")
			assert.Contains(t, abnormalLine, "[ERR]")
			assert.Contains(t, abnormalLine, firstByteRequestID)
			assert.Contains(t, abnormalLine, "channel_id=24")
			assert.Contains(t, abnormalLine, "proto=HTTP/1.1")
			for _, want := range tc.wantAbnormalLog {
				assert.Contains(t, abnormalLine, want)
			}
		})
	}
}

func TestOaiStreamHandlerBoundEOFStillFailsSettlementCompletion(t *testing.T) {
	// Arrange: a settlement-bound stream that ends before its first chunk.
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
	captureRelayLogs(t)
	resp := openFirstByteUpstream(t, writeSSE(""))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := newFirstByteRelayInfo(7)

	// Act
	usage, apiErr := OaiStreamHandler(c, info, resp)
	completionErr := service.SettlementStreamCompletionError(info)

	// Assert: the handler still settles as before and the fork's completion
	// fence still rejects the receipt without allowing a retry.
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.NotNil(t, completionErr)
	assert.Equal(t, types.ErrorCodeSettlementStreamIncomplete, completionErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(completionErr))
}

func TestParseUpstreamStreamErrorEvent(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantOK    bool
		wantType  string
		wantCode  string
		forbidden string
	}{
		{name: "content chunk", data: firstByteRoleChunk},
		{name: "usage chunk", data: firstByteUsageChunk},
		{name: "null error", data: `{"error":null,"choices":[]}`},
		{name: "object error", data: firstByteErrorEvent, wantOK: true, wantType: "server_error", wantCode: "upstream_timeout", forbidden: firstBytePrivateEchoed},
		{name: "numeric code", data: `{"error":{"message":"m","type":"api_error","code":503}}`, wantOK: true, wantType: "api_error", wantCode: "503"},
		{name: "string error", data: `{"error":"private prompt echo"}`, wantOK: true, forbidden: firstBytePrivateEchoed},
		{name: "unsafe code characters are replaced", data: `{"error":{"type":"bad type\n<script>","code":"a b"}}`, wantOK: true, wantType: "bad_type__script_", wantCode: "a_b"},
		{name: "long code is bounded", data: `{"error":{"type":"t","code":"` + strings.Repeat("x", 200) + `"}}`, wantOK: true, wantType: "t", wantCode: strings.Repeat("x", 64)},
		{name: "object code is not echoed", data: `{"error":{"type":"t","code":{"message":"private prompt echo"}}}`, wantOK: true, wantType: "t", forbidden: firstBytePrivateEchoed},
		{name: "content mentioning error is not an event", data: `{"choices":[{"index":0,"delta":{"content":"\"error\": nope"}}]}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, ok := parseUpstreamStreamErrorEvent(tc.data)

			require.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}
			assert.Equal(t, tc.wantType, event.Type)
			assert.Equal(t, tc.wantCode, event.Code)
			if tc.forbidden != "" {
				assert.NotContains(t, event.Type+event.Code, tc.forbidden)
			}
		})
	}
}

func newFirstByteRelayInfo(settlementBindingID int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat:         types.RelayFormatOpenAI,
		RelayMode:           relayconstant.RelayModeChatCompletions,
		IsStream:            true,
		ShouldIncludeUsage:  true,
		SettlementBindingId: settlementBindingID,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         firstByteChannelID,
			ChannelType:       constant.ChannelTypeDeepSeek,
			UpstreamModelName: "deepseek-v4-pro",
		},
	}
	info.SetEstimatePromptTokens(5)
	return info
}

func openFirstByteUpstream(t *testing.T, handler func(t *testing.T, w http.ResponseWriter, r *http.Request)) *http.Response {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(t, w, r)
	}))
	t.Cleanup(upstream.Close)

	request, err := http.NewRequest(http.MethodPost, upstream.URL+"/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	require.NoError(t, err)
	client := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return resp
}

// lockedLogBuffer serializes writes because the logger only holds the shared
// read side of common.LogWriterMu while writing.
type lockedLogBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// captureRelayLogs redirects the request logger for one test; the writer swap
// takes the write side of common.LogWriterMu.
func captureRelayLogs(t *testing.T) *lockedLogBuffer {
	t.Helper()
	logs := &lockedLogBuffer{}
	common.LogWriterMu.Lock()
	originalOut, originalErr := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = logs, logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = originalOut, originalErr
		common.LogWriterMu.Unlock()
	})
	return logs
}

func findLogLine(logText string, marker string) string {
	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}
