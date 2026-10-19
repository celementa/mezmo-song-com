package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/store"
)

const testWebhookSecret = "test-secret-for-order-ref"

func TestPublicOrderRefRoundTrip(t *testing.T) {
	ref := publicOrderRef(testWebhookSecret, "ord_abc123")
	id, ok := resolveOrderRef(testWebhookSecret, ref)
	if !ok {
		t.Fatalf("resolveOrderRef ok=false, want true")
	}
	if id != "ord_abc123" {
		t.Fatalf("id = %q, want ord_abc123", id)
	}
}

func TestResolveOrderRefRejectsForgedSignature(t *testing.T) {
	ref := publicOrderRef(testWebhookSecret, "ord_abc123")
	i := strings.LastIndex(ref, "-")
	sig := []byte(ref[i+1:])
	// Меняем один символ подписи на другой hex-символ.
	if sig[0] == 'a' {
		sig[0] = 'b'
	} else {
		sig[0] = 'a'
	}
	forged := ref[:i+1] + string(sig)

	if _, ok := resolveOrderRef(testWebhookSecret, forged); ok {
		t.Fatalf("resolveOrderRef ok=true for forged signature, want false")
	}
}

func TestResolveOrderRefRejectsOtherSecret(t *testing.T) {
	ref := publicOrderRef(testWebhookSecret, "ord_abc123")
	if _, ok := resolveOrderRef("other-secret", ref); ok {
		t.Fatalf("resolveOrderRef ok=true with a different secret, want false")
	}
}

func orderRefTestServer(t *testing.T) *Server {
	t.Helper()
	s, _ := testServer(t)
	s.Config = config.Config{
		PublicAppURL:  "http://localhost:8080",
		WebhookSecret: testWebhookSecret,
	}
	return s
}

func TestPublicOrderStatusRejectsBareIDAfterCutoff(t *testing.T) {
	s := orderRefTestServer(t)
	// Заказ, "созданный" на/после cutoff — уже должен требовать ref, а не голый id.
	seedOrderAt(t, s.DB, "ord_fresh1", "paid", orderRefCutoff.Add(1*time.Hour))

	req := httptest.NewRequest("GET", "/api/public/order/ord_fresh1/status", nil)
	req.SetPathValue("id", "ord_fresh1")
	rec := httptest.NewRecorder()
	s.handlePublicOrderStatus(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if _, has := out["paymentStatus"]; has {
		t.Fatalf("response contains order body: %v", out)
	}
}

func TestPublicOrderStatusAllowsBareIDForLegacyOrder(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrderAt(t, s.DB, "ord_legacy1", "paid", orderRefCutoff.Add(-24*time.Hour))

	req := httptest.NewRequest("GET", "/api/public/order/ord_legacy1/status", nil)
	req.SetPathValue("id", "ord_legacy1")
	rec := httptest.NewRecorder()
	s.handlePublicOrderStatus(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

// TestResolveOrderIDDecisionIsIndependentOfWallClock — обязательное доказательство,
// что resolveOrderID не зависит от того, какое сегодня число: сама функция ни разу не
// читает time.Now(), решение полностью определяется o.CreatedAt (данные из БД) и
// константой orderRefCutoff. Тест фиксирует это явно — на обеих сторонах границы —
// без подмены системных часов, потому что подмена не нужна: заменить здесь можно было
// бы разве что сам вызов time.Now(), а его в этом коде нет ни одного (см. orderref.go).
// Поэтому пакет останется зелёным и после 2026-08-15 — реальное «сейчас» на решение не
// влияет ни сегодня, ни тогда.
func TestResolveOrderIDDecisionIsIndependentOfWallClock(t *testing.T) {
	s := orderRefTestServer(t)

	// "До отсечки" — не имеет значения, что показывают часы в момент запроса.
	seedOrderAt(t, s.DB, "ord_clock_before", "paid", orderRefCutoff.Add(-time.Nanosecond))
	if _, ok := s.resolveOrderID(context.Background(), "ord_clock_before"); !ok {
		t.Fatalf("order created 1ns before cutoff must still resolve by bare id")
	}

	// Ровно на отсечке и позже — уже не legacy, независимо от реального "сейчас".
	seedOrderAt(t, s.DB, "ord_clock_at", "paid", orderRefCutoff)
	if _, ok := s.resolveOrderID(context.Background(), "ord_clock_at"); ok {
		t.Fatalf("order created exactly at cutoff must not resolve by bare id")
	}

	seedOrderAt(t, s.DB, "ord_clock_after", "paid", orderRefCutoff.Add(100*365*24*time.Hour))
	if _, ok := s.resolveOrderID(context.Background(), "ord_clock_after"); ok {
		t.Fatalf("order created far in the future (relative to cutoff) must not resolve by bare id")
	}
}

func TestPublicOrderStatusAcceptsRef(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrderAt(t, s.DB, "ord_legacy2", "paid", orderRefCutoff.Add(-24*time.Hour))
	ref := publicOrderRef(s.Config.WebhookSecret, "ord_legacy2")

	req := httptest.NewRequest("GET", "/api/public/order/"+ref+"/status", nil)
	req.SetPathValue("id", ref)
	rec := httptest.NewRecorder()
	s.handlePublicOrderStatus(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["id"] != ref {
		t.Fatalf("id = %v, want ref %q", out["id"], ref)
	}
	if out["trackingId"] != "ord_legacy2" {
		t.Fatalf("trackingId = %v, want raw id ord_legacy2", out["trackingId"])
	}
}

func TestPublicOrderContentTracksUseRef(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrderAt(t, s.DB, "ord_content1", "paid", orderRefCutoff.Add(-24*time.Hour))
	if err := store.SaveRelayResult(context.Background(), s.DB, "ord_content1", []string{"orders/ord_content1/track-1.mp3"}, "текст", ""); err != nil {
		t.Fatalf("save relay result: %v", err)
	}
	ref := publicOrderRef(s.Config.WebhookSecret, "ord_content1")

	req := httptest.NewRequest("GET", "/api/public/order/"+ref+"/content", nil)
	req.SetPathValue("id", ref)
	rec := httptest.NewRecorder()
	s.handlePublicOrderContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Orders []struct {
			Tracks []string `json:"tracks"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Orders) != 1 || len(out.Orders[0].Tracks) != 1 {
		t.Fatalf("unexpected orders payload: %+v", out)
	}
	track := out.Orders[0].Tracks[0]
	if !strings.Contains(track, ref) {
		t.Fatalf("track = %q, want it to contain ref %q", track, ref)
	}
	if strings.Contains(track, "ord_content1/audio") {
		t.Fatalf("track = %q, leaks raw id in the proxy path", track)
	}
}

func TestAudioProxyRejectsBareIDAfterCutoff(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrderAt(t, s.DB, "ord_fresh2", "paid", orderRefCutoff.Add(1*time.Hour))

	req := httptest.NewRequest("GET", "/api/public/order/ord_fresh2/audio/0", nil)
	req.SetPathValue("id", "ord_fresh2")
	req.SetPathValue("index", "0")
	rec := httptest.NewRecorder()
	s.handleAudioProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestOrdersDraftReturnsRef(t *testing.T) {
	s := orderRefTestServer(t)

	req := httptest.NewRequest("POST", "/api/orders/draft", strings.NewReader(`{"recipientName":"Ира","email":"a@b.ru"}`))
	rec := httptest.NewRecorder()
	s.handleOrdersDraft(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ref, _ := out["orderId"].(string)
	if ref == "" || !strings.Contains(ref, "-") {
		t.Fatalf("orderId = %q, want a ref (id-signature)", ref)
	}
	id, ok := resolveOrderRef(s.Config.WebhookSecret, ref)
	if !ok {
		t.Fatalf("orderId %q does not resolve as a ref", ref)
	}
	if _, err := store.GetOrder(context.Background(), s.DB, id); err != nil {
		t.Fatalf("order %s from ref not found: %v", id, err)
	}
}

func TestResolveOrderIDDoesNotTouchDBOnBadRef(t *testing.T) {
	s := orderRefTestServer(t)
	// "ord_doesnotexist" не заведён в БД, а подпись сфальсифицирована.
	badRef := "ord_doesnotexist-0000000000000000"

	req := httptest.NewRequest("GET", "/api/public/order/"+badRef+"/status", nil)
	req.SetPathValue("id", badRef)
	rec := httptest.NewRecorder()
	s.handlePublicOrderStatus(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404 (not 500)", rec.Code)
	}
	if _, err := store.GetOrder(context.Background(), s.DB, "ord_doesnotexist"); err != store.ErrNotFound {
		t.Fatalf("sanity: order unexpectedly exists or wrong error: %v", err)
	}
}

func TestInitiatePaymentRedirectReturnURLContainsRef(t *testing.T) {
	var capturedBody []byte
	s := yooRoutedServer(t,
		func(w http.ResponseWriter, id string) { writePaymentJSON(w, id, "pending", false) },
		func(w http.ResponseWriter, r *http.Request) {
			capturedBody, _ = io.ReadAll(r.Body)
			writeCreatedPaymentJSON(w, "pay_ref_1")
		},
	)
	s.Config.WebhookSecret = testWebhookSecret
	seedOrder(t, s.DB, "ord_returl", "pending")

	req := httptest.NewRequest("POST", "/api/orders/ord_returl/initiate-payment", strings.NewReader(`{"mode":"redirect"}`))
	req.SetPathValue("id", "ord_returl")
	rec := httptest.NewRecorder()
	s.handleInitiatePayment(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Confirmation struct {
			ReturnURL string `json:"return_url"`
		} `json:"confirmation"`
	}
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("decode captured payment payload: %v, body=%s", err, capturedBody)
	}
	ref := publicOrderRef(testWebhookSecret, "ord_returl")
	want := "/thanks?orderId=" + ref
	if !strings.HasSuffix(payload.Confirmation.ReturnURL, want) {
		t.Fatalf("return_url = %q, want suffix %q", payload.Confirmation.ReturnURL, want)
	}
}

func TestPublicOrderRefSurvivesEmailLink(t *testing.T) {
	s := orderRefTestServer(t)
	url := s.publicOrderURL("ord_email1")
	i := strings.LastIndex(url, "/")
	ref := url[i+1:]

	id, ok := s.resolveOrderID(context.Background(), ref)
	if !ok {
		t.Fatalf("publicOrderURL ref %q does not resolve", ref)
	}
	if id != "ord_email1" {
		t.Fatalf("resolved id = %q, want ord_email1", id)
	}
}
