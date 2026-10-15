package channel

import (
	"net"
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A settlement-bound request keeps the single physical send even on an HTTP/2
// path where the upstream resets the first stream or closes the connection
// after the body was written: the transport must not replay it.
func TestSettlementSingleWireDisablesHTTP2TransparentReplay(t *testing.T) {
	payload := []byte(`{"model":"test-model","messages":[{"role":"user","content":"bound"}]}`)

	for _, testCase := range []struct {
		name   string
		server func(net.Listener) <-chan h2ServerResult
	}{
		{name: "REFUSED_STREAM after the body", server: func(ln net.Listener) <-chan h2ServerResult { return runResetOnFirstStreamServer(ln, false) }},
		{name: "graceful GOAWAY after the body", server: runGoAwayAfterFirstRequestServer},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer ln.Close()
			resCh := testCase.server(ln)

			client, transport := newH2PriorKnowledgeClient(ln)
			defer transport.CloseIdleConnections()

			body, closer, err := relaycommon.NewOutboundJSONBody(payload)
			require.NoError(t, err)
			defer closer.Close()
			req, err := http.NewRequest(http.MethodPost, "http://upstream.test/v1/chat/completions", body)
			require.NoError(t, err)
			ApplyUpstreamBodyMetadata(req, body)
			require.NotNil(t, req.GetBody, "upstream wiring makes the body replayable")

			pinSettlementSingleWire(req)

			resp, err := client.Do(req) //nolint:bodyclose // Do fails, no body to close
			require.Error(t, err)
			assert.Nil(t, resp)
			// The GOAWAY server waits to accept a second connection; closing the
			// listener ends that wait once the client has given up.
			_ = ln.Close()
			srv := awaitH2ServerResult(t, resCh)
			assert.Equal(t, 1, srv.streamCount, "a bound request is sent exactly once")
		})
	}
}
