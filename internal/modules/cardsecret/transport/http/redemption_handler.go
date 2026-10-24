package cardsecrethttp

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/dujiao-next/internal/i18n"
	"github.com/dujiao-next/internal/modules/cardsecret/contract"
	"github.com/dujiao-next/internal/platform/http/response"
	"github.com/gin-gonic/gin"
)

type RedemptionService interface {
	Receive(contract.RedemptionReceipt) error
}

type RedemptionHandler struct {
	service RedemptionService
	enabled bool
	source  string
	secret  []byte
}

func NewRedemptionHandler(service RedemptionService, enabled bool, source, secret string) *RedemptionHandler {
	if enabled && (len(secret) < 32 || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(source)) {
		panic("card redemption callback requires a source and a secret of at least 32 bytes")
	}
	return &RedemptionHandler{service: service, enabled: enabled, source: source, secret: []byte(secret)}
}

func (h *RedemptionHandler) Receive(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	fail := func(status int, key string) {
		response.ErrorWithHTTPStatus(c, status, status, i18n.T(i18n.ResolveLocale(c), key))
	}
	if !h.enabled {
		fail(http.StatusServiceUnavailable, "error.redemption_disabled")
		return
	}
	timestamp := c.GetHeader("X-Redemption-Timestamp")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	now := time.Now().Unix()
	if err != nil || seconds < now-300 || seconds > now+300 {
		fail(http.StatusUnauthorized, "error.redemption_signature")
		return
	}
	signature, err := hex.DecodeString(c.GetHeader("X-Redemption-Signature"))
	if err != nil || len(signature) != sha256.Size {
		fail(http.StatusUnauthorized, "error.redemption_signature")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		fail(http.StatusBadRequest, "error.bad_request")
		return
	}
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		fail(http.StatusUnauthorized, "error.redemption_signature")
		return
	}
	var receipt contract.RedemptionReceipt
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		fail(http.StatusBadRequest, "error.card_secret_invalid")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(http.StatusBadRequest, "error.bad_request")
		return
	}
	if receipt.Source != h.source {
		fail(http.StatusUnauthorized, "error.redemption_signature")
		return
	}
	if err := h.service.Receive(receipt); err != nil {
		switch {
		case errors.Is(err, contract.ErrRedemptionInvalid):
			fail(http.StatusBadRequest, "error.card_secret_invalid")
		case errors.Is(err, contract.ErrRedemptionNotFound):
			fail(http.StatusNotFound, "error.card_secret_not_found")
		case errors.Is(err, contract.ErrRedemptionConflict):
			fail(http.StatusConflict, "error.redemption_conflict")
		default:
			fail(http.StatusServiceUnavailable, "error.card_secret_update_failed")
		}
		return
	}
	response.Success(c, gin.H{"event_id": receipt.EventID, "received": true})
}
