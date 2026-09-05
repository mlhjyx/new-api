package channel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBoundSettlementRequestCrossesPhysicalWireOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-first-wire?mode=memory&cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, model.EnsureSettlementReadbackSharedSchema(db))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })
	binding := &model.SettlementReadbackBinding{DispatchTokenId: 42, SettlementRequestIdSha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SettlementNonceSha256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", GatewayRequestId: "gateway-request", State: model.SettlementReadbackBindingBound, CreatedAt: 1}
	require.NoError(t, db.Create(binding).Error)
	service.InitHttpClient()

	var calls atomic.Int32
	var leaked atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Header.Get("X-New-API-Settlement-Request-Id") != "" || request.Header.Get("X-New-API-Settlement-Nonce") != "" {
			leaked.Store(true)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	info := &relaycommon.RelayInfo{
		SettlementBindingId:     binding.Id,
		SettlementDispatchFence: string(SettlementDispatchFenceOpenAIChat),
		ChannelMeta:             &relaycommon.ChannelMeta{},
	}
	request, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(nil))
	require.NoError(t, err)
	request.Header.Set("X-New-API-Settlement-Request-Id", "must-not-leak")
	request.Header.Set("X-New-API-Settlement-Nonce", "must-not-leak")
	response, err := DoRequest(context, request, info)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.EqualValues(t, 1, calls.Load())
	assert.False(t, leaked.Load())

	second, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(nil))
	require.NoError(t, err)
	_, err = DoRequest(context, second, info)
	assert.Error(t, err)
	assert.EqualValues(t, 1, calls.Load())
}
