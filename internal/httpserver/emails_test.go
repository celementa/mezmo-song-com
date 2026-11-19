package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/mail"
	"mezmo-song-com/internal/store"
)

// stubMail подменяет SMTP и считает отправки.
func stubMail(t *testing.T, fail error) *[]string {
	t.Helper()
	var sent []string
	prev := sendMailFunc
	sendMailFunc = func(_ config.Config, to []string, subject, _ string, _ mail.Tracking) error {
		if fail != nil {
			return fail
		}
		sent = append(sent, to[0]+"|"+subject)
		return nil
	}
	t.Cleanup(func() { sendMailFunc = prev })
	return &sent
}

func TestSendEmailRespectsToggleAndJournals(t *testing.T) {
	s, db := testServer(t)
	sent := stubMail(t, nil)
	ctx := context.Background()

	if err := s.sendEmail(ctx, emailSubReminder, emailTarget{SubscriptionID: "sub_1"}, "a@x.ru", "Скоро списание", "b", nil); err != nil {
		t.Fatalf("enabled by default: %v", err)
	}
	if err := store.SetEmailEnabled(ctx, db, emailSubReminder, false); err != nil {
		t.Fatal(err)
	}
	err := s.sendEmail(ctx, emailSubReminder, emailTarget{SubscriptionID: "sub_1"}, "a@x.ru", "Скоро списание", "b", nil)
	if !errors.Is(err, errEmailDisabled) {
		t.Fatalf("disabled email: want errEmailDisabled, got %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(*sent))
	}

	entries, total, err := store.ListEmailLog(ctx, db, "a@x.ru", 1, 50)
	if err != nil || total != 2 {
		t.Fatalf("journal: total=%d err=%v", total, err)
	}
	if entries[0].Status != store.EmailDisabled || entries[1].Status != store.EmailSent {
		t.Fatalf("journal statuses: %s, %s", entries[0].Status, entries[1].Status)
	}
	if entries[0].SubscriptionID != "sub_1" {
		t.Fatalf("journal subscription id: %q", entries[0].SubscriptionID)
	}
}

// Заблокированное письмо уходит даже при «выключенной» настройке в БД
// (например, выставленной SQL-ом руками), и ручка не даёт его выключить.
func TestLockedEmailCannotBeDisabled(t *testing.T) {
	s, db := testServer(t)
	sent := stubMail(t, nil)
	ctx := context.Background()

	_ = store.SetEmailEnabled(ctx, db, emailSongReady, false)
	if err := s.sendEmail(ctx, emailSongReady, emailTarget{OrderID: "ord_1"}, "a@x.ru", "Ваша песня готова", "b", nil); err != nil {
		t.Fatalf("locked email must be sent: %v", err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d, want 1", len(*sent))
	}

	req := httptest.NewRequest(http.MethodPut, "/api/admin/emails", strings.NewReader(`{"key":"song_ready","enabled":false}`))
	rec := httptest.NewRecorder()
	s.handleAdminEmailsPut(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "email_locked") {
		t.Fatalf("PUT locked: code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/emails", strings.NewReader(`{"key":"sub_charged","enabled":false}`))
	rec = httptest.NewRecorder()
	s.handleAdminEmailsPut(rec, req)
	if rec.Code != 200 || store.EmailEnabled(ctx, db, emailSubCharged) {
		t.Fatalf("PUT toggle: code=%d enabled=%v", rec.Code, store.EmailEnabled(ctx, db, emailSubCharged))
	}
}

func TestSendEmailJournalsFailure(t *testing.T) {
	s, db := testServer(t)
	stubMail(t, errors.New("550 spam"))
	ctx := context.Background()

	if err := s.sendEmail(ctx, emailPaymentReceived, emailTarget{OrderID: "ord_9"}, "b@x.ru", "Оплата получена", "b", nil); err == nil {
		t.Fatal("want error")
	}
	entries, _, _ := store.ListEmailLog(ctx, db, "ord_9", 1, 50)
	if len(entries) != 1 || entries[0].Status != store.EmailFailed || entries[0].Error != "550 spam" {
		t.Fatalf("journal: %+v", entries)
	}
}

// Английская подписка получает английское письмо, цену в долларах и ссылки
// на /en: иначе Stripe-подписчик видел «25 ₽» и русский кабинет.
func TestSubscriptionWelcomeEmailEnglish(t *testing.T) {
	s, db := testServer(t)
	s.Config.SMTPHost = "smtp.test"
	s.Config.SMTPFrom = "support@mezmo-song.com"
	s.Config.SMTPFromName = "Mezmo"
	s.Config.PublicAppURL = "https://mezmo-song.com"
	var gotSubject, gotBody string
	prev := sendMailFunc
	sendMailFunc = func(_ config.Config, _ []string, subject, body string, _ mail.Tracking) error {
		gotSubject, gotBody = subject, body
		return nil
	}
	t.Cleanup(func() { sendMailFunc = prev })

	ctx := t.Context()
	sub, err := store.UpsertPendingSubscriptionLang(ctx, db, "en@b.ru", "", store.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	periodEnd := time.Date(2026, 12, 19, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE subscriptions SET status = ?, provider = 'stripe', currency = 'USD',
		price_rub = 25, access_until = ?, next_charge_at = ? WHERE id = ?`,
		store.SubActive, periodEnd, periodEnd, sub.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.GetSubscription(ctx, db, sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.sendSubscriptionWelcomeEmail(fresh)

	if !strings.Contains(gotSubject, "Your subscription is active") {
		t.Fatalf("subject = %q", gotSubject)
	}
	for _, want := range []string{
		"$25",
		"https://mezmo-song.com/en/account",
		"/en/subscription/cancel?t=",
		"December 19, 2026",
	} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("body has no %q:\n%s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, "₽") {
		t.Errorf("в английском письме рублёвая цена:\n%s", gotBody)
	}
}

// Каждое письмо в реестре — с уникальным ключом и заполненными подсказками:
// пустой тултип в админке хуже, чем никакого.
func TestEmailKindsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range emailKinds {
		if seen[k.Key] {
			t.Errorf("duplicate key %s", k.Key)
		}
		seen[k.Key] = true
		if k.Title == "" || k.Subject == "" || k.When == "" || k.Why == "" || k.Group == "" {
			t.Errorf("incomplete kind %+v", k)
		}
	}
}
