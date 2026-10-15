package channel

import "net/http"

// pinSettlementSingleWire removes the transport-level replay source from a
// settlement-bound upstream request. Since upstream v1.0.0-rc.24 (d6b5ce99)
// DoApiRequest wires Request.GetBody so the HTTP/2 transport can resend an
// already written body after REFUSED_STREAM or a graceful GOAWAY. A bound
// request is admitted to exactly one physical send by
// BeginSettlementReadbackDispatch, so a transport reset must surface as a
// failure instead of a hidden second send inside client.Do.
func pinSettlementSingleWire(req *http.Request) {
	if req != nil {
		req.GetBody = nil
	}
}
