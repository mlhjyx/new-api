package router

// Fork routes that never reach dashboard authentication: settlement readers
// authenticate with SettlementReadbackAuth and the corresponding-source
// endpoint is public. They are exempt from the access-token route table.
func init() {
	accessTokenExemptRoutes = append(accessTokenExemptRoutes,
		"GET /api/settlement-readback/v1",
		"GET /api/settlement-readback/v1/capability",
		"GET /api/corresponding-source/v1",
	)
}
