package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/mail"
	"mezmo-song-com/internal/store"
)

const testTemplateID = "0f4a6b2c-1d3e-4f5a-8b9c-0d1e2f3a4b5c"

// templateCall — что sendEmail передал в шаблон.
type templateCall struct {
	to, templateID string
	vars           map[string]string
}

// stubTemplate подменяет отправку по шаблону; fail != nil — API отказал.
func stubTemplate(t *testing.T, fail error) *[]templateCall {
	t.Helper()
	var calls []templateCall
	prev := sendTemplateFunc
	sendTemplateFunc = func(_ config.Config, to, templateID string, vars map[string]string, _ mail.Tracking) error {
		calls = append(calls, templateCall{to, templateID, vars})
		return fail
	}
	t.Cleanup(func() { sendTemplateFunc = prev })
	return &calls
}

func goServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	s, db := testServer(t)
	s.Config = config.Config{SMTPUnisenderGo: true, PublicAppURL: "https://mezmo.ru/", SMTPFrom: "support@mezmo.ru"}
	return s, db
}

// С шаблоном письмо уходит через API Go с подстановками (свои + общие), SMTP
// не трогается, журнал — «отправлено» с ключом для вебхука.
func TestSendEmailUsesTemplate(t *testing.T) {
	s, db := goServer(t)
	ctx := context.Background()
	smtp := stubMail(t, nil)
	calls := stubTemplate(t, nil)
	_ = store.SetEmailTemplateID(ctx, db, emailSongReady, store.LangRU, testTemplateID)

	err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_1"}, "a@x.ru", "Ваша песня готова", "b",
		map[string]string{"song_url": "https://mezmo.ru/order/x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(*smtp) != 0 || len(*calls) != 1 {
		t.Fatalf("smtp=%d template=%d, want 0 and 1", len(*smtp), len(*calls))
	}
	c := (*calls)[0]
	if c.templateID != testTemplateID || c.vars["song_url"] != "https://mezmo.ru/order/x" ||
		c.vars["site_url"] != "https://mezmo.ru" || c.vars["support_email"] != "support@mezmo.ru" {
		t.Fatalf("call %+v", c)
	}
	entries, _, _ := store.ListEmailLog(ctx, db, "ord_1", 1, 50)
	if len(entries) != 1 || entries[0].Status != store.EmailSent || entries[0].Error != "" {
		t.Fatalf("journal %+v", entries)
	}
	var ref string
	if err := db.QueryRow(`SELECT ref FROM email_log WHERE order_id = 'ord_1'`).Scan(&ref); err != nil || ref == "" {
		t.Fatalf("journal ref %q err=%v — вебхуку Go не к чему привязать статус", ref, err)
	}
}

// Шаблон не принят — письмо всё равно уходит нашим текстом, причина в журнале.
func TestSendEmailTemplateFailureFallsBackToText(t *testing.T) {
	s, db := goServer(t)
	ctx := context.Background()
	smtp := stubMail(t, nil)
	stubTemplate(t, errors.New("unisender go: HTTP 400 code 114: template not found"))
	_ = store.SetEmailTemplateID(ctx, db, emailSongReady, store.LangRU, testTemplateID)

	if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_2"}, "a@x.ru", "Ваша песня готова", "b", nil); err != nil {
		t.Fatalf("fallback must succeed: %v", err)
	}
	if len(*smtp) != 1 {
		t.Fatalf("smtp sent %d, want 1", len(*smtp))
	}
	entries, _, _ := store.ListEmailLog(ctx, db, "ord_2", 1, 50)
	if len(entries) != 1 || entries[0].Status != store.EmailSent || !strings.Contains(entries[0].Error, "template not found") {
		t.Fatalf("journal %+v", entries)
	}
}

// Отклонённый Go адрес текстом не дублируется: Go отбросит и SMTP-письмо.
func TestSendEmailRejectedRecipientNoFallback(t *testing.T) {
	s, db := goServer(t)
	ctx := context.Background()
	smtp := stubMail(t, nil)
	stubTemplate(t, fmt.Errorf("%w: unsubscribed", mail.ErrRecipientRejected))
	_ = store.SetEmailTemplateID(ctx, db, emailSubCharged, store.LangRU, testTemplateID)

	if err := s.sendEmail(ctx, emailSubCharged, emailTarget{SubscriptionID: "sub_3"}, "a@x.ru", "Подписка продлена", "b", nil); err == nil {
		t.Fatal("want error")
	}
	if len(*smtp) != 0 {
		t.Fatalf("smtp sent %d, want 0", len(*smtp))
	}
	entries, _, _ := store.ListEmailLog(ctx, db, "sub_3", 1, 50)
	if len(entries) != 1 || entries[0].Status != store.EmailFailed {
		t.Fatalf("journal %+v", entries)
	}
}

// Без Unisender Go (обычный SMTP) шаблон игнорируется.
func TestSendEmailTemplateNeedsUnisenderGo(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	smtp := stubMail(t, nil)
	calls := stubTemplate(t, nil)
	_ = store.SetEmailTemplateID(ctx, db, emailSongReady, store.LangRU, testTemplateID)

	if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_4"}, "a@x.ru", "Ваша песня готова", "b", nil); err != nil {
		t.Fatal(err)
	}
	if len(*smtp) != 1 || len(*calls) != 0 {
		t.Fatalf("smtp=%d template=%d, want 1 and 0", len(*smtp), len(*calls))
	}
}

func TestAdminEmailsPutTemplate(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleAdminEmailsPut(rec, httptest.NewRequest(http.MethodPut, "/api/admin/emails", strings.NewReader(body)))
		return rec
	}

	// Шаблон можно задать и письму, которое нельзя выключить; регистр и пробелы чистятся.
	if rec := put(`{"key":"song_ready","templateId":" ` + strings.ToUpper(testTemplateID) + ` "}`); rec.Code != 200 {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	if got := store.EmailTemplateID(ctx, db, emailSongReady, store.LangRU); got != testTemplateID {
		t.Fatalf("stored %q", got)
	}
	if rec := put(`{"key":"song_ready","templateId":"не-uuid"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad_template_id") {
		t.Fatalf("bad id: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(`{"key":"song_ready","templateId":""}`); rec.Code != 200 || store.EmailTemplateID(ctx, db, emailSongReady, store.LangRU) != "" {
		t.Fatalf("clear: %d", rec.Code)
	}
	if rec := put(`{"key":"song_ready"}`); rec.Code != 400 {
		t.Fatalf("empty patch: %d", rec.Code)
	}
}

var templateVarRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Подстановки в реестре — с именами, которые принимает Unisender Go, и с
// описанием: по этому списку верстают шаблон.
func TestEmailKindsVarsValid(t *testing.T) {
	for _, k := range append(emailKinds, emailKind{Key: "common", Vars: commonEmailVars}) {
		if len(k.Vars) == 0 {
			t.Errorf("%s: нет подстановок", k.Key)
		}
		for _, v := range k.Vars {
			if !templateVarRe.MatchString(v.Name) || v.Desc == "" {
				t.Errorf("%s: подстановка %+v", k.Key, v)
			}
		}
	}
}

// Каждое письмо подписки передаёт все подстановки, объявленные в
// реестре: иначе в свёрстанном шаблоне останется пустое место.
func TestEmailVarsMatchRegistry(t *testing.T) {
	s, db := goServer(t)
	s.Config.SMTPHost = "smtp.test"
	ctx := context.Background()
	stubMail(t, nil)
	calls := stubTemplate(t, nil)
	for _, k := range emailKinds {
		_ = store.SetEmailTemplateID(ctx, db, k.Key, store.LangRU, testTemplateID)
	}

	sub := store.Subscription{ID: "sub_1", Email: "a@x.ru", CancelToken: "tok",
		NextChargeAt: sql.NullString{String: time.Now().Format(time.RFC3339), Valid: true},
		AccessUntil:  sql.NullString{String: time.Now().Format(time.RFC3339), Valid: true}}
	senders := map[string]func(){
		emailSubWelcome:      func() { s.sendSubscriptionWelcomeEmail(sub) },
		emailSubGift:         func() { _ = s.sendSubscriptionGiftEmail(sub) },
		emailSubReminder:     func() { _ = s.sendSubscriptionReminderEmail(sub) },
		emailSubCharged:      func() { s.sendSubscriptionChargedEmail(sub) },
		emailSubChargeFailed: func() { s.sendSubscriptionChargeFailedEmail(sub) },
		emailSubExpired:      func() { s.sendSubscriptionExpiredEmail(sub) },
		emailSubCancelLink:   func() { s.sendSubscriptionCancelLinkEmail(sub) },
	}
	for kind, send := range senders {
		*calls = nil
		send()
		if len(*calls) != 1 {
			t.Errorf("%s: отправок по шаблону %d", kind, len(*calls))
			continue
		}
		k, _ := emailKindByKey(kind)
		for _, v := range k.Vars {
			if _, ok := (*calls)[0].vars[v.Name]; !ok {
				t.Errorf("%s: в реестре {{%s}}, а письмо её не передаёт", kind, v.Name)
			}
		}
	}
}

// Английский заказ уходит по английскому шаблону, а пока его нет — текстом
// (английским, его собирает вызывающий), а не по русскому шаблону.
func TestSendEmailTemplateByOrderLanguage(t *testing.T) {
	s, db := goServer(t)
	ctx := context.Background()
	smtp := stubMail(t, nil)
	calls := stubTemplate(t, nil)
	const enTemplateID = "11111111-2222-3333-4444-555555555555"
	if err := store.CreateOrder(ctx, db, store.Order{ID: "ord_en", Status: "created", DeliveryStatus: store.DeliveryPending,
		PaymentStatus: store.PaymentPaid, InputPayloadJSON: "{}", CustomerJSON: "{}",
		Language: sql.NullString{String: store.LangEN, Valid: true}, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = store.SetEmailTemplateID(ctx, db, emailSongReady, store.LangRU, testTemplateID)

	if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_en"}, "a@x.com", "Your song is ready", "b", nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 || len(*smtp) != 1 {
		t.Fatalf("no EN template: template=%d smtp=%d, want 0 and 1", len(*calls), len(*smtp))
	}

	_ = store.SetEmailTemplateID(ctx, db, emailSongReady, store.LangEN, enTemplateID)
	if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_en"}, "a@x.com", "Your song is ready", "b", nil); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].templateID != enTemplateID {
		t.Fatalf("EN template calls %+v, want one with %s", *calls, enTemplateID)
	}
	if got := s.publicOrderURL("ord_en"); !strings.HasPrefix(got, "https://mezmo.ru/en/order/") {
		t.Fatalf("publicOrderURL = %q, want /en/order/ prefix", got)
	}
}
