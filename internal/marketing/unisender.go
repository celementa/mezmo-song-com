// Package marketing — события email-маркетинга в Unisender.
//
// Сервис не шлёт письма сам: он лишь заводит контакт в список и переключает
// поля-чекбоксы контакта, а цепочки писем настроены на стороне Unisender и
// срабатывают на смену поля (по окончании цепочки поле сбрасывает сам
// Unisender). Единственный метод API — subscribe с overwrite=2: переданные
// поля заменяются, остальные сохраняются, контакт добавляется без письма
// «подтвердите подписку» (double_optin=3).
//
// Все вызовы — best effort: ошибка логируется и никогда не влияет на заказ.
package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Поля контакта (имена полей в Unisender, см. ТЗ email-маркетинга).
const (
	FieldOrderPaid          = "order_pay"           // заказ оплачен → Flow 4
	FieldSongListened       = "song_listen"         // нажал «Прослушать» → Flow 6
	FieldSongDownloaded     = "song_donload"        // нажал «Скачать» (имя поля с опечаткой — как в Unisender)
	FieldSubscriber         = "subscriber"          // подписка активна
	FieldSubscriberCanceled = "subscriber_canceled" // подписка не продлена / отключена
)

const (
	defaultAPIBase = "https://api.unisender.com/ru/api/"
	requestTimeout = 10 * time.Second
)

type Client struct {
	APIKey  string
	ListID  string // список контактов, куда попадает каждый email
	APIBase string // пусто — боевой адрес
	HTTP    *http.Client
}

// Enabled — ключ задан; без ключа все вызовы молча пропускаются (dev, стенд).
func (c *Client) Enabled() bool { return c != nil && c.APIKey != "" }

// Subscribe заводит контакт в список (или обновляет существующий) и
// проставляет поля. fields — имя поля → значение; для чекбоксов "1"/"0".
func (c *Client) Subscribe(ctx context.Context, email string, fields map[string]string) error {
	if !c.Enabled() {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("unisender: пустой email")
	}
	form := url.Values{}
	form.Set("format", "json")
	form.Set("api_key", c.APIKey)
	form.Set("list_ids", c.ListID)
	form.Set("fields[email]", email)
	form.Set("double_optin", "3")
	form.Set("overwrite", "2")
	for k, v := range fields {
		form.Set("fields["+k+"]", v)
	}

	base := c.APIBase
	if base == "" {
		base = defaultAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/subscribe", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: requestTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("unisender: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var out struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(body, &out)
	if res.StatusCode >= 300 || out.Error != "" {
		return fmt.Errorf("unisender: http %d %s %s", res.StatusCode, out.Code, strings.TrimSpace(out.Error))
	}
	return nil
}

// Trigger — API-триггер сценария Unisender: POST JSON на URL блока
// («вызов по API» в омниканальном сценарии). В отличие от полей-чекбоксов
// событие приходит на каждый заказ отдельно, поэтому вторая песня одного
// контакта запускает цепочку снова. Ответ {"success":true}; иначе — ошибка.
func (c *Client) Trigger(ctx context.Context, triggerURL string, payload map[string]any) error {
	if strings.TrimSpace(triggerURL) == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, triggerURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: requestTimeout}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("unisender trigger: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var out struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Code    string `json:"code"`
	}
	_ = json.Unmarshal(raw, &out)
	if res.StatusCode >= 300 || out.Error != "" {
		return fmt.Errorf("unisender trigger: http %d %s %s", res.StatusCode, out.Code, strings.TrimSpace(out.Error))
	}
	return nil
}
