package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every attempt that returns the retryable first-byte 502 is never billed, so
// it is logged once with a fixed code and a process counter for reconciling
// upstream (openox) usage against new-api consume logs.
func TestFirstByteUnbilledAttemptsAreCountedAndLoggedWithAFixedCode(t *testing.T) {
	for _, tc := range []struct {
		name              string
		settlementBinding int
		pingBeforeStream  bool
		wantCounted       bool
	}{
		{name: "retryable eof before any chunk", wantCounted: true},
		{name: "keepalive already written is billed as today", pingBeforeStream: true},
		{name: "settlement-bound request keeps its own receipt path", settlementBinding: 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			originalTimeout := constant.StreamingTimeout
			constant.StreamingTimeout = 30
			t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
			logs := captureRelayLogs(t)
			resp := openFirstByteUpstream(t, writeSSE(""))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set(common.RequestIdKey, firstByteRequestID)
			c.Set(common.UpstreamRequestIdKey, "upstream-req-1")
			info := newFirstByteRelayInfo(tc.settlementBinding)
			info.RetryIndex = 1
			if tc.pingBeforeStream {
				_, err := c.Writer.WriteString(": PING\n\n")
				require.NoError(t, err)
				c.Writer.Flush()
			}
			before := FirstByteUnbilledAttempts()

			// Act
			_, _ = OaiStreamHandler(c, info, resp)

			// Assert
			line := findLogLine(logs.String(), FirstByteUnbilledLogCode)
			if !tc.wantCounted {
				assert.Empty(t, line)
				assert.Equal(t, before, FirstByteUnbilledAttempts())
				return
			}
			require.NotEmpty(t, line)
			assert.Equal(t, before+1, FirstByteUnbilledAttempts())
			for _, field := range []string{
				"request_id=" + firstByteRequestID,
				"upstream_request_id=upstream-req-1",
				"channel_id=24",
				"model=deepseek-v4-pro",
				"retry_index=1",
				"reason=eof",
				"upstream_error_code=-",
			} {
				assert.Contains(t, line, field)
			}
			assert.Regexp(t, `unbilled_attempts_total=\d+`, line)
		})
	}
}
