package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettlementCredentialLifecycleRejectsEveryAccessToken(t *testing.T) {
	for _, key := range []string{
		"POST /api/settlement-readback/admin/v1/credentials",
		"POST /api/settlement-readback/admin/v1/credentials/:id/rotate",
		"POST /api/settlement-readback/admin/v1/credentials/:id/revoke",
	} {
		rule, declared := AccessTokenRouteRule(key)
		require.True(t, declared, key)
		assert.Equal(t, "session", rule.Kind(), key)
	}
	for _, key := range []string{
		"GET /api/settlement-readback/v1",
		"GET /api/settlement-readback/v1/capability",
		"GET /api/corresponding-source/v1",
	} {
		_, declared := AccessTokenRouteRule(key)
		assert.False(t, declared, "%s is not a dashboard route", key)
	}
}
