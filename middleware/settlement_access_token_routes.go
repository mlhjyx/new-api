package middleware

// The settlement-readback credential lifecycle mints and rotates reader
// secrets, so it requires an interactive root session: every access token,
// legacy or scoped, is rejected there. Upstream's dashboard route table
// (access_token_routes.go) stays untouched to keep future syncs conflict-free.
//
// The reader routes (/api/settlement-readback/v1, /capability) and
// /api/corresponding-source/v1 never reach dashboard authentication: readers
// authenticate with SettlementReadbackAuth and the source endpoint is public.
func init() {
	for _, key := range []string{
		"POST /api/settlement-readback/admin/v1/credentials",
		"POST /api/settlement-readback/admin/v1/credentials/:id/rotate",
		"POST /api/settlement-readback/admin/v1/credentials/:id/revoke",
	} {
		if _, exists := accessTokenRouteRules[key]; exists {
			panic("access token route " + key + " is declared twice")
		}
		accessTokenRouteRules[key] = accessTokenSessionRule
	}
}
