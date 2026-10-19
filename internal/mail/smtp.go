package mail

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"mezmo-song-com/internal/config"
)

// SendPlain отправляет письмо, текст которого задан plain-текстом body.
//
// Наружу уходит multipart/alternative: тот же текст и HTML-версия, собранная из
// него (абзацы, кликабельные ссылки), у обеих — подпись с адресом поддержки.
// Плюс имя отправителя, Date и Message-ID. Всё это ради доставляемости:
// письмо из двух строк с голой ссылкой, без имени и подписи, mail.ru
// отбрасывал как спам (2026-09-16, `550 spam message rejected`).
//
// tr уходит только в Unisender Go (SMTPUnisenderGo): по нему вебхук Go
// сообщает, что стало с письмом. Для обычного SMTP игнорируется.
func SendPlain(cfg config.Config, to []string, subject, body string, tr Tracking) error {
	if cfg.SMTPHost == "" || cfg.SMTPFrom == "" || len(to) == 0 {
		return fmt.Errorf("mail: smtp not configured")
	}
	port := config.AtoiDef(cfg.SMTPPort, 587)
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, port)

	msg := buildMessage(cfg, to, subject, body, tr)

	auth := smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)

	// Try STARTTLS on 587
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: cfg.SMTPHost, InsecureSkipVerify: cfg.SMTPNoTLS}); err != nil {
			_ = client.Close()
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if cfg.SMTPUser != "" {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(cfg.SMTPFrom); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err = client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}
	return w.Close()
}

// buildMessage собирает письмо целиком (заголовки + multipart/alternative).
// Вынесено из SendPlain, чтобы тест мог проверить формат без SMTP.
func buildMessage(cfg config.Config, to []string, subject, body string, tr Tracking) string {
	support := cfg.MailReplyTo
	if support == "" {
		support = cfg.SMTPFrom
	}
	site := strings.TrimSuffix(cfg.PublicAppURL, "/")
	boundary := "=_mz_" + randomHex(8)

	text := body + "\n\n--\n" + cfg.SMTPFromName + " · " + site + "\nВопросы: " + support + "\n"
	unsubscribe := ""
	if cfg.SMTPUnisenderGo {
		unsubscribe = unisenderUnsubscribeURL
		text += "Отписаться от писем: " + unsubscribe + "\n"
	}

	var h strings.Builder
	h.WriteString("From: " + mime.QEncoding.Encode("utf-8", cfg.SMTPFromName) + " <" + cfg.SMTPFrom + ">\r\n")
	if cfg.MailReplyTo != "" {
		h.WriteString("Reply-To: " + cfg.MailReplyTo + "\r\n")
	}
	h.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	// Длинная кириллическая тема разбивается кодировщиком на несколько
	// encoded-word; складываем их по строкам, чтобы не выходить за 78 символов.
	h.WriteString("Subject: " + strings.ReplaceAll(mime.QEncoding.Encode("utf-8", subject), " =?", "\r\n =?") + "\r\n")
	h.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	h.WriteString("Message-ID: <" + randomHex(16) + "@" + domainOf(cfg.SMTPFrom) + ">\r\n")
	if cfg.SMTPUnisenderGo {
		h.WriteString("X-UNISENDER-GO: " + unisenderGoHeader(tr) + "\r\n")
	}
	h.WriteString("MIME-Version: 1.0\r\n")
	h.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")

	h.WriteString("--" + boundary + "\r\n")
	h.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	h.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	h.WriteString(wrapBase64(text))

	h.WriteString("--" + boundary + "\r\n")
	h.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	h.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	h.WriteString(wrapBase64(htmlBody(cfg.SMTPFromName, site, support, unsubscribe, body)))

	h.WriteString("--" + boundary + "--\r\n")
	return h.String()
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+`)

// Unisender Go: свою ссылку отписки он подставляет вместо тега, и тогда не
// добавляет стандартный блок.
const unisenderUnsubscribeURL = "{{UnsubscribeUrl}}"

// Tracking — метки письма для Unisender Go.
type Tracking struct {
	Ref string // ключ строки email_log; Go возвращает его в вебхуке (metadata.ref)
	Tag string // тип письма — метка в отчётах кабинета Go (до 50 символов)
}

// unisenderGoHeader — опции заголовка X-UNISENDER-GO (те же, что у email/send):
// ссылки ведут прямо на сайт (track_links=0 включён поддержкой 2026-10-12),
// прочтения считаются (track_read), страница отписки на русском.
func unisenderGoHeader(tr Tracking) string {
	opts := map[string]any{"track_links": 0, "track_read": 1, "global_language": "ru"}
	if tr.Ref != "" {
		opts["global_metadata"] = map[string]string{"ref": tr.Ref}
	}
	if tr.Tag != "" {
		opts["tags"] = []string{tr.Tag}
	}
	b, _ := json.Marshal(opts)
	return string(b)
}

// htmlBody — HTML-версия plain-текста: пустая строка = новый абзац, перенос =
// <br>, ссылки кликабельны. Внизу подпись с сайтом и адресом поддержки и,
// если unsubscribe не пуст, ссылка отписки.
func htmlBody(fromName, site, support, unsubscribe, body string) string {
	var paras []string
	for _, p := range strings.Split(strings.TrimSpace(body), "\n\n") {
		esc := html.EscapeString(strings.TrimSpace(p))
		esc = urlRe.ReplaceAllStringFunc(esc, func(u string) string {
			return `<a href="` + u + `" style="color:#1a56db">` + u + `</a>`
		})
		paras = append(paras, `<p style="margin:0 0 16px">`+strings.ReplaceAll(esc, "\n", "<br>")+`</p>`)
	}
	return `<!DOCTYPE html><html><head><meta charset="utf-8"></head>` +
		`<body style="margin:0;padding:24px;background:#ffffff;font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;font-size:16px;line-height:1.5;color:#1f2937">` +
		`<div style="max-width:560px;margin:0 auto">` +
		strings.Join(paras, "") +
		`<hr style="border:0;border-top:1px solid #e5e7eb;margin:24px 0">` +
		`<p style="margin:0;font-size:13px;color:#6b7280">` + html.EscapeString(fromName) +
		` · <a href="` + site + `" style="color:#6b7280">` + html.EscapeString(strings.TrimPrefix(strings.TrimPrefix(site, "https://"), "http://")) + `</a><br>` +
		`Вопросы: <a href="mailto:` + support + `" style="color:#6b7280">` + html.EscapeString(support) + `</a>` +
		unsubscribeHTML(unsubscribe) + `</p>` +
		`</div></body></html>`
}

func wrapBase64(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.String()
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func domainOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		return addr[i+1:]
	}
	return "localhost"
}

func unsubscribeHTML(u string) string {
	if u == "" {
		return ""
	}
	return `<br><a href="` + u + `" style="color:#9ca3af">Отписаться от писем</a>`
}
