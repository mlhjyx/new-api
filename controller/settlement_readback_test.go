package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSettlementReadbackCapabilityFailsClosedWithoutTransactionalTopology(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1/capability", nil)
	GetSettlementReadbackCapability(context)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, settlementReadbackContract, recorder.Header().Get("X-New-API-Settlement-Contract"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestSettlementReadbackCannotFallbackToBroadLogsBeforeBindingContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settlement-readback/v1", nil)
	GetSettlementReadback(context)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Empty(t, recorder.Body.String())
}

func TestSettlementReadbackRootCredentialLifecycleReturnsSecretOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:settlement-readback-controller?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.SettlementReadbackCredential{}))
	require.NoError(t, db.Create(&model.Token{Id: 42, UserId: 7, Key: "controller-reader-token", Status: common.TokenStatusEnabled}).Error)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB })

	keyringPath := filepath.Join(t.TempDir(), "keyring")
	require.NoError(t, os.WriteFile(keyringPath, []byte("schema=settlement-readback-pepper-keyring/v1\npepper-v1 ACTIVE AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"), 0o600))
	t.Setenv(settlementReadbackPepperKeyringFileEnv, keyringPath)

	createRecorder := httptest.NewRecorder()
	createContext, _ := gin.CreateTestContext(createRecorder)
	createContext.Request = httptest.NewRequest(http.MethodPost, "/api/settlement-readback/admin/v1/credentials", bytes.NewBufferString(`{"dispatch_token_id":42}`))
	CreateSettlementReadbackCredential(createContext)
	require.Equal(t, http.StatusCreated, createRecorder.Code)
	var created map[string]interface{}
	require.NoError(t, common.Unmarshal(createRecorder.Body.Bytes(), &created))
	data := created["data"].(map[string]interface{})
	secret := data["credential"].(string)
	credentialID := int(data["credential_id"].(float64))
	assert.NotEmpty(t, secret)
	assert.Equal(t, "no-store", createRecorder.Header().Get("Cache-Control"))
	var stored model.SettlementReadbackCredential
	require.NoError(t, db.First(&stored, credentialID).Error)
	assert.NotEqual(t, secret, stored.SecretDigest)

	rotateRecorder := httptest.NewRecorder()
	rotateContext, _ := gin.CreateTestContext(rotateRecorder)
	rotateContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(credentialID)}}
	rotateContext.Request = httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewBufferString(`{"overlap_seconds":300}`))
	RotateSettlementReadbackCredential(rotateContext)
	require.Equal(t, http.StatusCreated, rotateRecorder.Code)
	assert.NotContains(t, rotateRecorder.Body.String(), secret)

	revokeRecorder := httptest.NewRecorder()
	revokeContext, _ := gin.CreateTestContext(revokeRecorder)
	revokeContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(credentialID)}}
	revokeContext.Request = httptest.NewRequest(http.MethodPost, "/revoke", nil)
	RevokeSettlementReadbackCredential(revokeContext)
	require.Equal(t, http.StatusOK, revokeRecorder.Code)
}
