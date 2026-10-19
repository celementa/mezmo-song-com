package httpserver

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/mail"
	"mezmo-song-com/internal/store"
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

func TestUnisenderGoWebhookTracksLesrosesMail(t *testing.T) {
	s, db, _ := lesrosesServer(t)
	s.Config.SMTPUnisenderGo = true
	s.Config.SMTPPassword = "go-key"
	var refs []string
	prev := sendMailFunc
	sendMailFunc = func(_ config.Config, _ []string, _, _ string, tr mail.Tracking) error {
		if tr.Ref == "" || tr.Tag != emailLesrosesLink {
			t.Errorf("письмо через Go без ключа или метки: %+v", tr)
		}
		refs = append(refs, tr.Ref)
		return nil
	}
	t.Cleanup(func() { sendMailFunc = prev })

	ctx := context.Background()
	_, id, err := s.createLesrosesOrder(ctx, store.PartnerOrderRef{Number: "6100"}, "a@b.ru", store.ArmLesrosesPopup, 990)
	if err != nil {
		t.Fatal(err)
	}
	waitEmails(t, db, emailLesrosesLink, 1)
	ref := refs[0]

	mux := http.NewServeMux()
	s.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/webhooks/unisender-go", nil))
	if rec.Code != 200 {
		t.Fatalf("GET-проверка адреса для webhook/set: %d", rec.Code)
	}

	if code := postGoWebhook(t, s, goCallback("wrong-key", goEvent(ref, "delivered", ""))); code != 401 {
		t.Fatalf("чужая подпись: want 401, got %d", code)
	}
	// Открытие предзагрузкой (Apple Mail) приходит раньше доставки — порядок
	// событий Go не гарантирует, итог от него зависеть не должен.
	if code := postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "opened", "protected"), goEvent("other", "delivered", ""))); code != 200 {
		t.Fatalf("webhook: %d", code)
	}
	if code := postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "delivered", ""))); code != 200 {
		t.Fatalf("webhook: %d", code)
	}

	rows, err := store.ListPartnerOrders(ctx, db, store.FlowLesroses, time.Now().Add(-time.Hour), "", 10)
	if err != nil || len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("rows: %+v err=%v", rows, err)
	}
	r := rows[0]
	if !r.MailTracked || !r.MailDelivered || r.MailOpenedAt == "" || r.MailOpenedHumanAt != "" {
		t.Fatalf("после предзагрузки: tracked=%v delivered=%v opened=%q human=%q", r.MailTracked, r.MailDelivered, r.MailOpenedAt, r.MailOpenedHumanAt)
	}

	// Человек открыл позже — первое «человеческое» открытие пишется, повтор ничего не меняет.
	postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "opened", "human")))
	postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "opened", "human")))
	rows, _ = store.ListPartnerOrders(ctx, db, store.FlowLesroses, time.Now().Add(-time.Hour), "", 10)
	if rows[0].MailOpenedHumanAt != "2026-10-14T09:30:00Z" {
		t.Fatalf("human open: %q", rows[0].MailOpenedHumanAt)
	}

	// Спам окончательный: поздняя «доставка» его не перекрывает.
	postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "spam", "")))
	postGoWebhook(t, s, goCallback("go-key", goEvent(ref, "delivered", "")))
	rows, _ = store.ListPartnerOrders(ctx, db, store.FlowLesroses, time.Now().Add(-time.Hour), "", 10)
	if rows[0].MailProblem != store.DeliverySpam {
		t.Fatalf("spam: problem=%q", rows[0].MailProblem)
	}
}

func TestUnisenderGoWebhookOffWithoutGo(t *testing.T) {
	s, _ := testServer(t)
	if code := postGoWebhook(t, s, goCallback("", goEvent("x", "delivered", ""))); code != 503 {
		t.Fatalf("без SMTP_UNISENDER_GO: want 503, got %d", code)
	}
}

func TestBriefProgress(t *testing.T) {
	s, db, _ := lesrosesServer(t)
	ctx := context.Background()
	_, id, _ := s.createLesrosesOrder(ctx, store.PartnerOrderRef{Number: "6200"}, "a@b.ru", store.ArmLesrosesCart, 990)
	ref := publicOrderRef(s.Config.WebhookSecret, id)
	mux := http.NewServeMux()
	s.Register(mux)
	post := func(target, body string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/public/order/"+target+"/brief-progress", strings.NewReader(body)))
		return rec.Code
	}
	row := func() store.PartnerOrderRow {
		rows, err := store.ListPartnerOrders(ctx, db, store.FlowLesroses, time.Now().Add(-time.Hour), "", 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows: %v %v", rows, err)
		}
		return rows[0]
	}

	if r := row(); r.BriefOpenedAt != "" || r.BriefStep != 0 {
		t.Fatalf("до открытия: %+v", r)
	}
	post(ref, `{"step":1}`)
	post(ref, `{"step":3}`)
	post(ref, `{"step":2}`) // «Назад» прогресс не откатывает
	post(ref, `{"step":9}`) // мусор игнорируется
	if code := post(id+"-bad", `{"step":4}`); code != 204 {
		t.Fatalf("неверная подпись ref: %d", code)
	}
	r := row()
	if r.BriefOpenedAt == "" || r.BriefStep != 3 || r.BriefStepAt == "" {
		t.Fatalf("после шагов: opened=%q step=%d at=%q", r.BriefOpenedAt, r.BriefStep, r.BriefStepAt)
	}

	// Анкета отправлена — отметки больше не пишутся.
	if _, err := db.Exec(`UPDATE orders SET input_payload = '{"recipientName":"Ира"}' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	post(ref, `{"step":4}`)
	if r := row(); r.BriefStep != 3 {
		t.Fatalf("после анкеты шаг изменился: %d", r.BriefStep)
	}

	// Кабинет отдаёт ход анкеты по заказу.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/admin/lesroses/orders", nil))
	var out struct {
		Orders []struct {
			Brief map[string]any `json:"brief"`
			Mail  map[string]any `json:"mail"`
		} `json:"orders"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Orders) != 1 || out.Orders[0].Brief["step"] != float64(3) || out.Orders[0].Mail["tracked"] != false {
		t.Fatalf("admin brief: %v", out.Orders[0].Brief)
	}
}
