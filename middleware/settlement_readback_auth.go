package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	SettlementReadbackReaderIDContextKey             = "settlement_readback_reader_id"
	SettlementReadbackDispatchTokenContextKey        = "settlement_readback_dispatch_token_id"
	settlementReadbackCredentialPepperKeyringFileEnv = "SETTLEMENT_READBACK_CREDENTIAL_PEPPER_KEYRING_FILE"
	settlementReadbackAuthorizationMaxBytes          = 4096
)

func settlementReadbackDeny(c *gin.Context) {
	c.AbortWithStatus(http.StatusUnauthorized)
}

func SettlementReadbackAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if len(authorization) >= settlementReadbackAuthorizationMaxBytes {
			settlementReadbackDeny(c)
			return
		}
		if !strings.HasPrefix(authorization, "Bearer ") {
			settlementReadbackDeny(c)
			return
		}
		secret := strings.TrimPrefix(authorization, "Bearer ")
		if secret == "" || strings.TrimSpace(secret) != secret {
			settlementReadbackDeny(c)
			return
		}
		keyring, err := model.LoadSettlementReadbackPepperKeyring(os.Getenv(settlementReadbackCredentialPepperKeyringFileEnv))
		if err != nil {
			settlementReadbackDeny(c)
			return
		}
		credential, err := model.GetUsableSettlementReadbackCredential(secret, keyring, time.Now().Unix())
		if err != nil {
			settlementReadbackDeny(c)
			return
		}
		token, err := model.GetTokenById(credential.DispatchTokenId)
		if err != nil || token.Status == common.TokenStatusDisabled {
			settlementReadbackDeny(c)
			return
		}
		user, err := model.GetUserCache(token.UserId)
		if err != nil || user.Status != common.UserStatusEnabled {
			settlementReadbackDeny(c)
			return
		}
		c.Set(SettlementReadbackReaderIDContextKey, credential.Id)
		c.Set(SettlementReadbackDispatchTokenContextKey, credential.DispatchTokenId)
		c.Next()
	}
}

func denyBoundDispatchTokenLegacyRead(c *gin.Context, tokenID int) bool {
	bound, err := model.HasActiveSettlementReadbackCredential(tokenID)
	if err != nil {
		settlementReadbackDeny(c)
		return true
	}
	if !bound {
		return false
	}
	settlementReadbackDeny(c)
	return true
}
