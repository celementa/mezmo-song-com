package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func stripeSignature(payload []byte, secret string, at time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", at.Unix())
	mac.Write(payload)
	return "t=" + strconv.FormatInt(at.Unix(), 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyStripeSignature(t *testing.T) {
	payload := []byte(`{"id":"evt_1"}`)
	secret := "whsec_test"
	now := time.Now()
	header := stripeSignature(payload, secret, now)
	if err := VerifyStripeSignature(payload, header, secret, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifyStripeSignature([]byte(`{"id":"evt_2"}`), header, secret, now); err == nil {
		t.Fatal("signature must not match another payload")
	}
	if err := VerifyStripeSignature(payload, header, "wrong", now); err == nil {
		t.Fatal("wrong secret must be rejected")
	}
	old := now.Add(-10 * time.Minute)
	if err := VerifyStripeSignature(payload, stripeSignature(payload, secret, old), secret, now); err == nil {
		t.Fatal("old signature must be rejected")
	}
	if err := VerifyStripeSignature(payload, "garbage", secret, now); err == nil {
		t.Fatal("malformed header must be rejected")
	}
}

func TestCreateCheckoutSessionUsesEmbeddedPage(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"id":"cs_test_1","mode":"payment","status":"open","payment_status":"unpaid","client_secret":"cs_test_1_secret_x","currency":"usd","amount_total":1900}`))
	}))
	defer srv.Close()
	old := StripeAPIBaseURL
	StripeAPIBaseURL = srv.URL
	defer func() { StripeAPIBaseURL = old }()

	res, err := (Stripe{SecretKey: "sk_test"}).CreateCheckoutSession(StripeCheckoutRequest{
		OrderID:           "ord_1",
		Mode:              StripeModePayment,
		PriceID:           "price_1",
		ReturnURL:         "https://mezmo-song.com/ru/order/ord_1",
		Locale:            "ru",
		SavePaymentMethod: true,
		Metadata:          map[string]string{"order_id": "ord_1"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.ClientSecret != "cs_test_1_secret_x" {
		t.Fatalf("client_secret = %q", res.ClientSecret)
	}
	if got.Get("ui_mode") != "embedded_page" {
		t.Fatalf("ui_mode = %q, want embedded_page (embedded больше не поддерживается)", got.Get("ui_mode"))
	}
	if got.Get("mode") != "payment" || got.Get("line_items[0][price]") != "price_1" {
		t.Fatalf("form = %v", got)
	}
	if got.Get("return_url") == "" || got.Get("metadata[order_id]") != "ord_1" {
		t.Fatalf("return_url/metadata not sent: %v", got)
	}
	if got.Get("customer_creation") != "always" || got.Get("payment_intent_data[setup_future_usage]") != "off_session" {
		t.Fatalf("save payment method not requested: %v", got)
	}
}

func TestCreateSubscriptionSessionSetsTrialAndMetadata(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		_, _ = w.Write([]byte(`{"id":"cs_sub","mode":"subscription","status":"open","payment_status":"unpaid","client_secret":"cs_sub_secret","currency":"usd","amount_total":1500}`))
	}))
	defer srv.Close()
	old := StripeAPIBaseURL
	StripeAPIBaseURL = srv.URL
	defer func() { StripeAPIBaseURL = old }()

	_, err := (Stripe{SecretKey: "sk_test"}).CreateCheckoutSession(StripeCheckoutRequest{
		OrderID:   "ord_1",
		Mode:      StripeModeSubscription,
		PriceID:   "price_sub",
		ReturnURL: "https://mezmo-song.com/en/order/ord_1",
		TrialDays: 30,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.Get("mode") != "subscription" {
		t.Fatalf("mode = %q", got.Get("mode"))
	}
	if got.Get("subscription_data[trial_period_days]") != "30" {
		t.Fatalf("trial = %q", got.Get("subscription_data[trial_period_days]"))
	}
	if got.Get("subscription_data[metadata][order_id]") != "ord_1" {
		t.Fatalf("subscription metadata = %v", got)
	}
	if strings.Contains(got.Encode(), "payment_intent_data") {
		t.Fatal("subscription session must not contain payment_intent_data")
	}
}
