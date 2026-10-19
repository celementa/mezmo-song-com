package mail

import (
	"strings"
	"testing"

	"mezmo-song-com/internal/config"
)

func TestBuildMessage(t *testing.T) {
	cfg := config.Config{
		SMTPFrom: "support@mezmo.ru", SMTPFromName: "Mezmo",
		PublicAppURL: "https://mezmo.ru/",
	}
	msg := buildMessage(cfg, []string{"a@b.ru"}, "Ваша песня готова", "Здравствуйте!\n\nСсылка:\nhttps://mezmo.ru/order/x", Tracking{})

	head, rest, _ := strings.Cut(msg, "\r\n\r\n")
	for _, want := range []string{
		"From: Mezmo <support@mezmo.ru>",
		"Subject: =?utf-8?q?",
		"Date: ", "Message-ID: <", "@mezmo.ru>",
		"Content-Type: multipart/alternative; boundary=",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("headers lack %q:\n%s", want, head)
		}
	}
	if !strings.Contains(rest, "text/plain; charset=UTF-8") || !strings.Contains(rest, "text/html; charset=UTF-8") {
		t.Errorf("both parts expected:\n%s", rest)
	}
	// RFC 5322: жёсткий предел строки 998, base64-тело режется по 76.
	for _, l := range strings.Split(msg, "\r\n") {
		if len(l) > 998 {
			t.Errorf("line longer than 998: %q", l)
		}
	}

	h := htmlBody("Mezmo", "https://mezmo.ru", "support@mezmo.ru", "", "Здравствуйте!\n\nСсылка:\nhttps://mezmo.ru/order/x <b>")
	for _, want := range []string{
		`<p style="margin:0 0 16px">Здравствуйте!</p>`,
		`Ссылка:<br><a href="https://mezmo.ru/order/x"`,
		"&lt;b&gt;",
		`mailto:support@mezmo.ru`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("html lacks %q:\n%s", want, h)
		}
	}
}

func TestBuildMessageUnisenderGo(t *testing.T) {
	cfg := config.Config{
		SMTPFrom: "support@mezmo.ru", SMTPFromName: "Mezmo",
		PublicAppURL: "https://mezmo.ru",
	}
	plain := buildMessage(cfg, []string{"a@b.ru"}, "Тема", "Текст", Tracking{Ref: "abc", Tag: "order_ready"})
	if strings.Contains(plain, "X-UNISENDER-GO") {
		t.Errorf("обычный SMTP не должен получать заголовок Go:\n%s", plain)
	}

	cfg.SMTPUnisenderGo = true
	msg := buildMessage(cfg, []string{"a@b.ru"}, "Тема", "Текст", Tracking{Ref: "abc", Tag: "order_ready"})
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	want := `X-UNISENDER-GO: {"global_language":"ru","global_metadata":{"ref":"abc"},"tags":["order_ready"],"track_links":0,"track_read":1}`
	if !strings.Contains(head, want) {
		t.Errorf("нет заголовка Go:\n%s", head)
	}
	// Go требует резать заголовок длиннее 300 байт — наш короче, резать нечего.
	if len(want) > 300 {
		t.Errorf("заголовок Go длиннее 300 байт: %d", len(want))
	}
	if strings.Contains(unisenderGoHeader(Tracking{}), "global_metadata") || strings.Contains(unisenderGoHeader(Tracking{}), "tags") {
		t.Errorf("пустые метки не должны попадать в заголовок: %s", unisenderGoHeader(Tracking{}))
	}
	h := htmlBody("Mezmo", "https://mezmo.ru", "support@mezmo.ru", unisenderUnsubscribeURL, "Текст")
	if !strings.Contains(h, `<a href="{{UnsubscribeUrl}}"`) {
		t.Errorf("html без ссылки отписки:\n%s", h)
	}
}
