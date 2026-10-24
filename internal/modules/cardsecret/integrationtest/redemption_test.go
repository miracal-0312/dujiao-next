package integrationtest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/cardsecret/application/redemption"
	"github.com/dujiao-next/internal/modules/cardsecret/contract"
	"github.com/dujiao-next/internal/modules/cardsecret/domain"
	"github.com/dujiao-next/internal/modules/cardsecret/infrastructure/gormstore"
	cardsecrethttp "github.com/dujiao-next/internal/modules/cardsecret/transport/http"
	"github.com/gin-gonic/gin"
)

func TestRedemptionCallback(t *testing.T) {
	db := setupCardSecretServiceTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	store := gormstore.New(db)
	key := strings.Repeat("s", 32)
	handler := cardsecrethttp.NewRedemptionHandler(redemption.New(store), true, "pay-gpt-orbit", key)
	router := gin.New()
	router.POST("/callback", handler.Receive)
	orderID := uint(123)
	usedAt := time.Now().Add(-time.Hour).UTC()
	item := domain.Secret{ProductID: 1, Secret: "CODE-TEST", Status: domain.StatusUsed, OrderID: &orderID, UsedAt: &usedAt}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	receipt := contract.RedemptionReceipt{Version: 1, EventID: strings.Repeat("a", 64), EventType: "cdk.redeemed",
		Source: "pay-gpt-orbit", CDK: "CODE-TEST", OrderID: "job-1", RedeemedAt: time.Now().UTC().Truncate(time.Millisecond)}
	send := func(value contract.RedemptionReceipt, seconds int64, tamper bool) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		timestamp := strconv.FormatInt(seconds, 10)
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(timestamp + "."))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		if tamper {
			body = append(body, ' ')
		}
		request := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Redemption-Timestamp", timestamp)
		request.Header.Set("X-Redemption-Signature", signature)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, tc := range []struct {
		name    string
		seconds int64
		tamper  bool
	}{
		{"tampered", time.Now().Unix(), true}, {"expired", time.Now().Unix() - 301, false},
		{"future", time.Now().Unix() + 301, false},
	} {
		if result := send(receipt, tc.seconds, tc.tamper); result.Code != 401 {
			t.Fatalf("%s: %d %s", tc.name, result.Code, result.Body)
		}
	}
	var unchanged domain.Secret
	if err := db.First(&unchanged, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.RedeemedAt != nil {
		t.Fatal("invalid signatures mutated card")
	}
	for n := 0; n < 2; n++ {
		result := send(receipt, time.Now().Unix(), false)
		if result.Code != 200 {
			t.Fatalf("receive: %d %s", result.Code, result.Body)
		}
		var ack struct {
			StatusCode int `json:"status_code"`
			Data       struct {
				EventID  string `json:"event_id"`
				Received bool   `json:"received"`
			} `json:"data"`
		}
		if err := json.Unmarshal(result.Body.Bytes(), &ack); err != nil {
			t.Fatal(err)
		}
		if ack.StatusCode != 0 || !ack.Data.Received || ack.Data.EventID != receipt.EventID {
			t.Fatalf("bad acknowledgement: %s", result.Body)
		}
		if result.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("callback response is cacheable")
		}
	}
	var updated domain.Secret
	if err := db.First(&updated, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.RedeemedAt == nil || !updated.RedeemedAt.Equal(receipt.RedeemedAt) || updated.Status != domain.StatusUsed ||
		updated.OrderID == nil || *updated.OrderID != orderID || !updated.UsedAt.Equal(usedAt) {
		t.Fatalf("incorrect card state: %+v", updated)
	}
	unchanged.Status = domain.StatusAvailable
	if err := store.Update(&unchanged); err == nil {
		t.Fatal("stale admin edit erased receipt")
	}
	if count, err := store.BatchUpdateStatus([]uint{item.ID}, domain.StatusAvailable, time.Now()); err != nil || count != 0 {
		t.Fatal("redeemed card returned to inventory")
	}
	if err := db.First(&updated, item.ID).Error; err != nil || updated.RedeemedAt == nil || updated.Status != domain.StatusUsed {
		t.Fatal("receipt not preserved")
	}
	for _, field := range []string{"event", "order", "source", "time"} {
		conflict := receipt
		switch field {
		case "event":
			conflict.EventID = strings.Repeat("b", 64)
		case "order":
			conflict.OrderID = "job-other"
		case "source":
			conflict.Source = "another-source"
		case "time":
			conflict.RedeemedAt = receipt.RedeemedAt.Add(-time.Second)
		}
		want := 409
		if field == "source" {
			want = 401
		}
		if result := send(conflict, time.Now().Unix(), false); result.Code != want {
			t.Fatalf("%s conflict: %d", field, result.Code)
		}
	}
	for _, tc := range []struct {
		code, status string
		count, want  int
	}{
		{"CODE-AVAILABLE", domain.StatusAvailable, 1, 409}, {"CODE-RESERVED", domain.StatusReserved, 1, 409},
		{"CODE-DUPLICATE", domain.StatusUsed, 2, 409}, {"CODE-MISSING", domain.StatusUsed, 0, 404},
		{"CODE-OTHER", domain.StatusUsed, 1, 409},
	} {
		for n := 0; n < tc.count; n++ {
			card := domain.Secret{ProductID: 1, Secret: tc.code, Status: tc.status}
			if err := db.Create(&card).Error; err != nil {
				t.Fatal(err)
			}
		}
		other := receipt
		other.CDK = tc.code
		if result := send(other, time.Now().Unix(), false); result.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.code, result.Code, result.Body)
		}
	}
	for _, change := range []func(*contract.RedemptionReceipt){
		func(r *contract.RedemptionReceipt) { r.Version = 2 },
		func(r *contract.RedemptionReceipt) { r.EventType = "cdk.pending" },
		func(r *contract.RedemptionReceipt) { r.RedeemedAt = time.Time{} },
		func(r *contract.RedemptionReceipt) { r.CDK = "bad'code" },
	} {
		invalid := receipt
		change(&invalid)
		if result := send(invalid, time.Now().Unix(), false); result.Code != 400 {
			t.Fatalf("invalid payload: %d", result.Code)
		}
	}
	disabled := gin.New()
	disabled.POST("/callback", cardsecrethttp.NewRedemptionHandler(redemption.New(store), false, "", "").Receive)
	result := httptest.NewRecorder()
	disabled.ServeHTTP(result, httptest.NewRequest(http.MethodPost, "/callback", nil))
	if result.Code != 503 {
		t.Fatal("disabled receiver accepted a callback")
	}
}
