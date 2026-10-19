package mail

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/config"
)

func stubGoAPI(t *testing.T, status int, response string) *map[string]any {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/email/send.json" || r.Header.Get("X-API-KEY") != "go-key" {
			t.Errorf("request %s key=%q", r.URL.Path, r.Header.Get("X-API-KEY"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	prev := UnisenderGoAPI
	UnisenderGoAPI = srv.URL
	t.Cleanup(func() { UnisenderGoAPI = prev })
	return &got
}

var goCfg = config.Config{SMTPPassword: "go-key", SMTPFrom: "support@mezmo.ru", SMTPFromName: "Mezmo"}

func TestSendTemplateRequest(t *testing.T) {
	got := stubGoAPI(t, 200, `{"status":"success","job_id":"1","emails":["a@x.ru"]}`)
	err := SendTemplate(goCfg, "a@x.ru", "tpl-1", map[string]string{"song_url": "https://mezmo.ru/order/x"}, Tracking{Ref: "r1", Tag: "song_ready"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := (*got)["message"].(map[string]any)
	b, _ := json.Marshal(msg)
	for _, want := range []string{
		`"template_id":"tpl-1"`, `"template_engine":"simple"`, `"from_email":"support@mezmo.ru"`,
		`"track_links":0`, `"track_read":1`, `"global_metadata":{"ref":"r1"}`, `"tags":["song_ready"]`,
		`"recipients":[{"email":"a@x.ru","substitutions":{"song_url":"https://mezmo.ru/order/x"}}]`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("message has no %s: %s", want, b)
		}
	}
	// Тема и тело — из шаблона: переданные явно перекрыли бы его.
	if _, ok := msg["subject"]; ok {
		t.Error("subject must come from template")
	}
	if _, ok := msg["body"]; ok {
		t.Error("body must come from template")
	}
}

func TestSendTemplateErrors(t *testing.T) {
	stubGoAPI(t, 400, `{"status":"error","message":"Template not found","code":114}`)
	if err := SendTemplate(goCfg, "a@x.ru", "tpl-1", nil, Tracking{}); err == nil || !strings.Contains(err.Error(), "Template not found") {
		t.Fatalf("api error: %v", err)
	}

	stubGoAPI(t, 200, `{"status":"success","emails":[],"failed_emails":{"a@x.ru":"unsubscribed"}}`)
	if err := SendTemplate(goCfg, "a@x.ru", "tpl-1", nil, Tracking{}); !errors.Is(err, ErrRecipientRejected) {
		t.Fatalf("rejected recipient: %v", err)
	}
}
