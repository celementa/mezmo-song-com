package httpserver

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// goCallback собирает тело вебхука Go и подписывает его так же, как Go:
// MD5 тела, в котором на месте auth стоит API-ключ.
func goCallback(key string, events ...string) string {
	tmpl := `{"auth":"%AUTH%","events_by_user":[{"user_id":1,"events":[` + strings.Join(events, ",") + `]}]}`
	sum := md5.Sum([]byte(strings.Replace(tmpl, "%AUTH%", key, 1)))
	return strings.Replace(tmpl, "%AUTH%", hex.EncodeToString(sum[:]), 1)
}

func goEvent(ref, status, traffic string) string {
	return `{"event_name":"transactional_email_status","event_data":{"job_id":"j","metadata":{"ref":"` + ref +
		`"},"email":"a@b.ru","status":"` + status + `","event_time":"2026-10-14 09:30:00","delivery_info":{"traffic_type":"` + traffic + `"}}}`
}

func postGoWebhook(t *testing.T, s *Server, body string) int {
	t.Helper()
	mux := http.NewServeMux()
	s.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/webhooks/unisender-go", strings.NewReader(body)))
	return rec.Code
}

func TestUnisenderGoWebhookOffWithoutGo(t *testing.T) {
	s, _ := testServer(t)
	if code := postGoWebhook(t, s, goCallback("", goEvent("x", "delivered", ""))); code != 503 {
		t.Fatalf("без SMTP_UNISENDER_GO: want 503, got %d", code)
	}
}
