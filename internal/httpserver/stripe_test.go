package httpserver

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/store"
)

func stripeTestServer(t *testing.T, handler http.HandlerFunc) *Server {
	t.Helper()
	s, db := providerTestServer(t)
	s.Config.StripeSecretKey = "sk_test"
	s.Config.StripePublishableKey = "pk_test"
	s.Config.StripeWebhookSecret = "whsec_test"
	s.Config.StripePriceV3Onetime = "price_19"
	s.Config.StripePriceV3Subscription = "price_15"
	s.Config.StripePrice990Onetime = "price_25"
	s.Config.StripePrice990Subscription = "price_25sub"

	api := httptest.NewServer(handler)
	t.Cleanup(api.Close)
	old := payment.StripeAPIBaseURL
	payment.StripeAPIBaseURL = api.URL
	t.Cleanup(func() { payment.StripeAPIBaseURL = old })
	_ = db
	return s
}

func seedStripeOrder(t *testing.T, s *Server) store.Order {
	t.Helper()
	seedOrder(t, s.DB, "ord_stripe", store.PaymentPending)
	_, err := s.DB.Exec(`UPDATE orders SET flow_variant = ?, payment_branch = 'intl', currency = 'USD',
		payment_plan = 'onetime', base_price = 19 WHERE id = ?`, ArmCom500750s, "ord_stripe")
	if err != nil {
		t.Fatal(err)
	}
	o, err := store.GetOrder(t.Context(), s.DB, "ord_stripe")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func stripeWebhookRequest(t *testing.T, secret string, payload []byte) *http.Request {
	t.Helper()
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", ts)
	mac.Write(payload)
	req := httptest.NewRequest("POST", "/api/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", "t="+strconv.FormatInt(ts, 10)+",v1="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

// Встроенная форма Stripe создаётся на нашем домене: сервер отдаёт client_secret
// и publishable key, а не уводит покупателя на чужую страницу.
func TestInitiateStripeReturnsClientSecret(t *testing.T) {
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"cs_test","client_secret":"cs_test_secret","mode":"payment","status":"open","payment_status":"unpaid","currency":"usd","amount_total":1900}`))
	})
	o := seedStripeOrder(t, s)

	rec := httptest.NewRecorder()
	s.initiateStripe(rec, httptest.NewRequest("POST", "/", nil), o, "https://mezmo-song.com/order/x")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["clientSecret"] != "cs_test_secret" || out["provider"] != ProviderStripe {
		t.Fatalf("response = %v", out)
	}
	if out["publishableKey"] != "pk_test" {
		t.Fatalf("publishableKey = %v", out["publishableKey"])
	}
}

// Платёж зачитывается только по вебхуку и только после перечитывания сессии:
// подпись проверена, сумма и валюта сверены с заказом.
func TestStripeWebhookSettlesPaidOrder(t *testing.T) {
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/checkout/sessions":
			_, _ = w.Write([]byte(`{"id":"cs_test","client_secret":"cs_test_secret","mode":"payment","status":"open","payment_status":"unpaid","currency":"usd","amount_total":1900}`))
		case r.URL.Path == "/v1/checkout/sessions/cs_test":
			_, _ = w.Write([]byte(`{"id":"cs_test","mode":"payment","status":"complete","payment_status":"paid","currency":"usd","amount_total":1900,"metadata":{"order_id":"ord_stripe"},"payment_intent":"pi_test","customer":"cus_test"}`))
		case r.URL.Path == "/v1/payment_intents/pi_test":
			_, _ = w.Write([]byte(`{"id":"pi_test","status":"succeeded","customer":"cus_test","payment_method":"pm_test","amount":1900,"currency":"usd"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	o := seedStripeOrder(t, s)
	rec := httptest.NewRecorder()
	s.initiateStripe(rec, httptest.NewRequest("POST", "/", nil), o, "https://mezmo-song.com/order/x")
	if rec.Code != 200 {
		t.Fatalf("initiate status = %d", rec.Code)
	}

	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_test"}}}`)
	wrec := httptest.NewRecorder()
	s.handleStripeWebhook(wrec, stripeWebhookRequest(t, "whsec_test", payload))
	if wrec.Code != 200 {
		t.Fatalf("webhook status = %d", wrec.Code)
	}
	got, err := store.GetOrder(t.Context(), s.DB, "ord_stripe")
	if err != nil {
		t.Fatal(err)
	}
	if got.PaymentStatus != store.PaymentPaid {
		t.Fatalf("payment_status = %q, want paid", got.PaymentStatus)
	}
	if got.PaymentProvider.String != ProviderStripe {
		t.Fatalf("provider = %q, want stripe", got.PaymentProvider.String)
	}
	if got.StripeCustomerID.String != "cus_test" || got.StripePaymentMethodID.String != "pm_test" {
		t.Fatalf("stripe customer/pm = %q/%q, want cus_test/pm_test", got.StripeCustomerID.String, got.StripePaymentMethodID.String)
	}
}

func TestStripeWebhookRejectsBadSignature(t *testing.T) {
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("API must not be called for an unsigned webhook")
	})
	seedStripeOrder(t, s)
	payload := []byte(`{"id":"evt_bad","type":"checkout.session.completed","data":{"object":{"id":"cs_test"}}}`)
	req := httptest.NewRequest("POST", "/api/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", "t=1,v1=deadbeef")
	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// Сумма и валюта из сессии сверяются с заказом: $19 нельзя зачесть как 19 ₽
// или как другой тариф.
func TestStripeWebhookRejectsWrongAmount(t *testing.T) {
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"cs_test","mode":"payment","status":"complete","payment_status":"paid","currency":"usd","amount_total":2500,"metadata":{"order_id":"ord_stripe"},"payment_intent":"pi_test"}`))
	})
	seedStripeOrder(t, s)
	payload := []byte(`{"id":"evt_amt","type":"checkout.session.completed","data":{"object":{"id":"cs_test"}}}`)
	rec := httptest.NewRecorder()
	s.handleStripeWebhook(rec, stripeWebhookRequest(t, "whsec_test", payload))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (no retry for a business rejection)", rec.Code)
	}
	got, _ := store.GetOrder(t.Context(), s.DB, "ord_stripe")
	if got.PaymentStatus == store.PaymentPaid {
		t.Fatal("order must not be paid when session amount differs")
	}
}

// Подписка Stripe: первый платёж засчитывается заказу, локальная подписка
// получает provider=stripe и доступ до current_period_end. Продления потом
// ведёт Stripe, а не локальный планировщик.
func TestStripeSubscriptionCheckoutActivatesLocalSubscription(t *testing.T) {
	periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/checkout/sessions":
			_, _ = w.Write([]byte(`{"id":"cs_sub","client_secret":"cs_sub_secret","mode":"subscription","status":"open","payment_status":"unpaid","currency":"usd","amount_total":1500}`))
		case "/v1/checkout/sessions/cs_sub":
			_, _ = w.Write([]byte(`{"id":"cs_sub","mode":"subscription","status":"complete","payment_status":"paid","currency":"usd","amount_total":1500,"metadata":{"order_id":"ord_sub"},"subscription":"sub_1","customer":"cus_1"}`))
		case "/v1/subscriptions/sub_1":
			_, _ = fmt.Fprintf(w, `{"id":"sub_1","status":"active","currency":"usd","current_period_end":%d,"customer":"cus_1"}`, periodEnd)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	seedOrder(t, s.DB, "ord_sub", store.PaymentPending)
	if _, err := s.DB.Exec(`UPDATE orders SET flow_variant = ?, payment_branch = 'intl', currency = 'USD',
		payment_plan = 'subscription', base_price = 15 WHERE id = ?`, ArmCom500750s, "ord_sub"); err != nil {
		t.Fatal(err)
	}
	o, _ := store.GetOrder(t.Context(), s.DB, "ord_sub")
	rec := httptest.NewRecorder()
	s.initiateStripe(rec, httptest.NewRequest("POST", "/", nil), o, "https://mezmo-song.com/order/x")
	if rec.Code != 200 {
		t.Fatalf("initiate status = %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := []byte(`{"id":"evt_sub","type":"checkout.session.completed","data":{"object":{"id":"cs_sub"}}}`)
	wrec := httptest.NewRecorder()
	s.handleStripeWebhook(wrec, stripeWebhookRequest(t, "whsec_test", payload))
	if wrec.Code != 200 {
		t.Fatalf("webhook status = %d", wrec.Code)
	}
	order, _ := store.GetOrder(t.Context(), s.DB, "ord_sub")
	if order.PaymentStatus != store.PaymentPaid {
		t.Fatalf("order payment_status = %q", order.PaymentStatus)
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "a@b.ru")
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	if sub.Provider.String != "stripe" || sub.StripeSubscriptionID.String != "sub_1" {
		t.Fatalf("subscription provider/id = %q/%q", sub.Provider.String, sub.StripeSubscriptionID.String)
	}
	if sub.Status != store.SubActive {
		t.Fatalf("subscription status = %q", sub.Status)
	}
	if sub.AccessUntilTime().Unix() != periodEnd {
		t.Fatalf("access_until = %v, want %v", sub.AccessUntilTime().Unix(), periodEnd)
	}
}

// После разовой оплаты карта сохранена: апсейл включает подписку напрямую,
// без второго Checkout и повторного ввода данных.
func TestStripeUpsellReusesSavedPaymentMethod(t *testing.T) {
	periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
	s := stripeTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/subscriptions":
			_ = r.ParseForm()
			if r.PostForm.Get("customer") != "cus_test" || r.PostForm.Get("default_payment_method") != "pm_test" {
				t.Fatalf("subscription form = %v", r.PostForm)
			}
			if r.PostForm.Get("trial_period_days") != "30" {
				t.Fatalf("trial = %q", r.PostForm.Get("trial_period_days"))
			}
			_, _ = fmt.Fprintf(w, `{"id":"sub_direct","status":"trialing","currency":"usd","current_period_end":%d,"customer":"cus_test"}`, periodEnd)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	seedOrder(t, s.DB, "ord_upsell", store.PaymentPaid)
	if _, err := s.DB.Exec(`UPDATE orders SET flow_variant = ?, payment_branch = 'intl', currency = 'USD',
		base_price = 25, payment_provider = 'stripe', stripe_customer_id = 'cus_test',
		stripe_payment_method_id = 'pm_test' WHERE id = ?`, ArmCom990, "ord_upsell"); err != nil {
		t.Fatal(err)
	}
	o, _ := store.GetOrder(t.Context(), s.DB, "ord_upsell")

	rec := httptest.NewRecorder()
	s.initiateStripeUpsell(rec, httptest.NewRequest("POST", "/", nil), o)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["activated"] != true || out["subscriptionId"] != "sub_direct" {
		t.Fatalf("response = %v", out)
	}
	if _, has := out["clientSecret"]; has {
		t.Fatalf("must not ask for card again: %v", out)
	}
	sub, err := store.GetSubscriptionByEmail(t.Context(), s.DB, "a@b.ru")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Provider.String != "stripe" || sub.StripeSubscriptionID.String != "sub_direct" || sub.Status != store.SubActive {
		t.Fatalf("subscription = provider %q id %q status %q", sub.Provider.String, sub.StripeSubscriptionID.String, sub.Status)
	}
}
