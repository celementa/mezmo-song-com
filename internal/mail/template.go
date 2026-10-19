package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"mezmo-song-com/internal/config"
)

// UnisenderGoAPI — Web API Unisender Go. Аккаунт mezmo живёт на go2 (тот же
// датацентр, что SMTP smtp.go2.unisender.ru); переменная — ради тестов.
var UnisenderGoAPI = "https://go2.unisender.ru/ru/transactional/api/v1"

var templateClient = &http.Client{Timeout: 20 * time.Second}

// ErrRecipientRejected — Go не принял адрес (отписан, жаловался на спам,
// невалиден). Слать этому адресу текстом через SMTP бессмысленно: Go его
// так же отбросит, только молча.
var ErrRecipientRejected = errors.New("unisender go: адрес отклонён")

// SendTemplate отправляет письмо по шаблону из конструктора Unisender Go
// (email/send с template_id). Тема и тело берутся из шаблона, vars уходят
// подстановками получателя ({{name}} в шаблоне, шаблонизатор simple).
// Отправитель — тот же, что у SMTP: шаблон его не задаёт, а адрес вне
// подтверждённого домена Go отклонил бы. Ключ API — SMTP_PASSWORD: у Go это
// один и тот же ключ.
func SendTemplate(cfg config.Config, to, templateID string, vars map[string]string, tr Tracking) error {
	if cfg.SMTPPassword == "" || cfg.SMTPFrom == "" {
		return fmt.Errorf("mail: unisender go not configured")
	}
	if vars == nil {
		vars = map[string]string{}
	}
	msg := map[string]any{
		"recipients":      []map[string]any{{"email": to, "substitutions": vars}},
		"template_id":     templateID,
		"template_engine": "simple",
		"from_email":      cfg.SMTPFrom,
		"from_name":       cfg.SMTPFromName,
		// Те же опции, что у SMTP-писем (unisenderGoHeader): ссылки без
		// редиректа Go, прочтения считаются, страница отписки на русском.
		"track_links":     0,
		"track_read":      1,
		"global_language": "ru",
	}
	if cfg.MailReplyTo != "" {
		msg["reply_to"] = cfg.MailReplyTo
	}
	if tr.Ref != "" {
		msg["global_metadata"] = map[string]string{"ref": tr.Ref}
	}
	if tr.Tag != "" {
		msg["tags"] = []string{tr.Tag}
	}
	payload, _ := json.Marshal(map[string]any{"message": msg})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UnisenderGoAPI+"/email/send.json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", cfg.SMTPPassword)
	resp, err := templateClient.Do(req)
	if err != nil {
		return fmt.Errorf("unisender go: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var out struct {
		Status       string            `json:"status"`
		Message      string            `json:"message"`
		Code         int               `json:"code"`
		FailedEmails map[string]string `json:"failed_emails"`
	}
	_ = json.Unmarshal(body, &out)
	if resp.StatusCode != http.StatusOK || out.Status != "success" {
		if out.Message != "" {
			return fmt.Errorf("unisender go: HTTP %d code %d: %s", resp.StatusCode, out.Code, out.Message)
		}
		return fmt.Errorf("unisender go: HTTP %d: %.200s", resp.StatusCode, body)
	}
	// Адрес, отклонённый Go (отписан, жаловался, невалиден), приходит в
	// failed_emails при общем success — письмо ему не ушло.
	if reason, failed := out.FailedEmails[to]; failed {
		return fmt.Errorf("%w: %s", ErrRecipientRejected, reason)
	}
	return nil
}
