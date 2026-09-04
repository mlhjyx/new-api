package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestSettlementReceiptPersistenceErrorIsPostWireAndNeverRetryable(t *testing.T) {
	err := SettlementReceiptPersistenceError()
	assert.Equal(t, types.ErrorCodeSettlementPersistenceFailed, err.GetErrorCode())
	assert.Equal(t, http.StatusInternalServerError, err.StatusCode)
	assert.True(t, types.IsSkipRetryError(err))
	assert.NotContains(t, err.Error(), "sql")
	assert.NotContains(t, err.Error(), "credential")
}
