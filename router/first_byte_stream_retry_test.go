package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	plainRelayTokenID  = 43
	plainRelayTokenKey = "plainrelaytoken"
	plainRelayUserID   = 7
)

func plainRelayChunk(content string, finishReason string) string {
	finish := "null"
	if finishReason != "" {
		finish = `"` + finishReason + `"`
	}
	return `data: {"id":"chatcmpl-plain","object":"chat.completion.chunk","created":1710000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"` + content + `"},"finish_reason":` + finish + `}]}` + "\n\n"
}

func plainRelayCompleteStream() string {
	return plainRelayChunk("retried-", "") +
		plainRelayChunk("answer", "stop") +
		`data: {"id":"chatcmpl-plain","object":"chat.completion.chunk","created":1710000000,"model":"gpt-4o-mini","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}` + "\n\n" +
		"data: [DONE]\n\n"
}

// TestUnboundStreamFailureBeforeFirstByteIsRetriedWithoutBilling drives the real
// relay router with a token that has no settlement credential: a stream that
// fails before any byte reaches the client must go back to the retry loop, and
// only the attempt that is actually delivered may be billed.
func TestUnboundStreamFailureBeforeFirstByteIsRetriedWithoutBilling(t *testing.T) {
	failedFirstAttempts := []struct {
		name   string
		body   string
		marker string
	}{
		{name: "eof before any chunk", body: ""},
		{name: "error event then close", body: `data: {"error":{"message":"upstream overloaded","type":"server_error","code":"upstream_timeout"}}` + "\n\n", marker: "upstream_timeout"},
		{name: "one content chunk then eof", body: plainRelayChunk("first-attempt-fragment", ""), marker: "first-attempt-fragment"},
	}

	for index, attempt := range failedFirstAttempts {
		t.Run(attempt.name+" with one retry", func(t *testing.T) {
			// Arrange
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				call := upstreamCalls.Add(1)
				writer.Header().Set("Content-Type", "text/event-stream")
				if call == 1 {
					_, _ = writer.Write([]byte(attempt.body))
					return
				}
				_, _ = writer.Write([]byte(plainRelayCompleteStream()))
			}))
			defer upstream.Close()
			_, engine := newSettlementDispatchIntegrationHarness(t, fmt.Sprintf("first-byte-retry-%d", index), upstream.URL, constant.ChannelTypeOpenAI, "")
			createPlainRelayToken(t)
			setRetryTimes(t, 1)

			// Act
			response := performPlainStreamRequest(engine)

			// Assert
			require.Equal(t, http.StatusOK, response.Code)
			assert.EqualValues(t, 2, upstreamCalls.Load())
			assert.Contains(t, response.Body.String(), `"content":"retried-"`)
			assert.Contains(t, response.Body.String(), `"content":"answer"`)
			assert.Contains(t, response.Body.String(), "data: [DONE]")
			if attempt.marker != "" {
				assert.NotContains(t, response.Body.String(), attempt.marker, "the failed attempt must not leak into the delivered stream")
			}
			waitForRelayWorkers(t)
			consumeLogs := plainRelayConsumeLogs(t)
			require.Len(t, consumeLogs, 1, "only the delivered attempt is billed")
			assert.Equal(t, 2, consumeLogs[0].CompletionTokens)
		})

		t.Run(attempt.name+" without retries", func(t *testing.T) {
			// Arrange
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(attempt.body))
			}))
			defer upstream.Close()
			_, engine := newSettlementDispatchIntegrationHarness(t, fmt.Sprintf("first-byte-no-retry-%d", index), upstream.URL, constant.ChannelTypeOpenAI, "")
			createPlainRelayToken(t)
			setRetryTimes(t, 0)
			quotaBefore := plainRelayUserQuota(t)

			// Act
			response := performPlainStreamRequest(engine)

			// Assert
			require.Equal(t, http.StatusBadGateway, response.Code)
			assert.EqualValues(t, 1, upstreamCalls.Load())
			assert.Contains(t, response.Body.String(), string(types.ErrorCodeUpstreamStreamAbortedBeforeFirstByte))
			assert.NotContains(t, response.Body.String(), "data: ")
			waitForRelayWorkers(t)
			assert.Empty(t, plainRelayConsumeLogs(t), "a failed attempt must not record a consume log")
			assert.Equal(t, quotaBefore, plainRelayUserQuota(t), "a failed attempt must not be billed")
		})
	}
}

func TestUnboundStreamFailureAfterFirstByteIsNotRetried(t *testing.T) {
	// Arrange: the first chunk is written to the client before the stream breaks.
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(plainRelayChunk("Hel", "") + plainRelayChunk("lo", "")))
	}))
	defer upstream.Close()
	_, engine := newSettlementDispatchIntegrationHarness(t, "first-byte-after-write", upstream.URL, constant.ChannelTypeOpenAI, "")
	createPlainRelayToken(t)
	setRetryTimes(t, 1)

	// Act
	response := performPlainStreamRequest(engine)

	// Assert: today's behaviour is kept once bytes reached the client.
	require.Equal(t, http.StatusOK, response.Code)
	assert.EqualValues(t, 1, upstreamCalls.Load())
	assert.Contains(t, response.Body.String(), `"content":"Hel"`)
	assert.Contains(t, response.Body.String(), "data: [DONE]")
	waitForRelayWorkers(t)
	assert.Len(t, plainRelayConsumeLogs(t), 1)
}

func createPlainRelayToken(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Token{
		Id:             plainRelayTokenID,
		UserId:         plainRelayUserID,
		Key:            plainRelayTokenKey,
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    1_000_000_000,
		UnlimitedQuota: true,
		Group:          "default",
	}).Error)
}

func setRetryTimes(t *testing.T, retryTimes int) {
	t.Helper()
	originalRetries := common.RetryTimes
	common.RetryTimes = retryTimes
	t.Cleanup(func() { common.RetryTimes = originalRetries })
}

func performPlainStreamRequest(engine http.Handler) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer sk-"+plainRelayTokenKey)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func waitForRelayWorkers(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		return gopool.WorkerCount() == 0
	}, 5*time.Second, time.Millisecond)
}

func plainRelayConsumeLogs(t *testing.T) []model.Log {
	t.Helper()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", plainRelayTokenID, model.LogTypeConsume).Find(&logs).Error)
	return logs
}

func plainRelayUserQuota(t *testing.T) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", plainRelayUserID).First(&user).Error)
	return user.Quota
}
