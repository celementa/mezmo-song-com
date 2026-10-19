package httpserver

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"mezmo-song-com/internal/marketing"
	"mezmo-song-com/internal/store"
)

// Точки, откуда уходят события email-маркетинга в Unisender (ТЗ 2026-09-08):
//   - каждый новый email → контакт в списке (черновик заказа, старт /create,
//     email после превью, оформление подписки);
//   - заказ оплачен → order_pay=1 (вебхук ЮKassa, песня по подписке);
//   - «Прослушать» / «Скачать» → song_listen / song_donload (ручка engagement);
//   - подписка активна → subscriber=1; отключилась → subscriber=0,
//     subscriber_canceled=1.
// Всё асинхронно и best effort: маркетинг не имеет права ломать заказ.

func (s *Server) unisender() *marketing.Client {
	return &marketing.Client{APIKey: s.Config.UnisenderAPIKey, ListID: s.Config.UnisenderListID, APIBase: s.Config.UnisenderAPIBase}
}

// marketingMu выстраивает отправки в очередь. Два `subscribe` по одному
// контакту в одну секунду (подписка активирована + заказ оплачен) Unisender
// принимал оба с ok, но второй перезаписывал контакт поверх снимка без поля
// первого — `subscriber=1` терялся, хотя overwrite=2 обещает трогать только
// переданные поля (прод, 2026-09-23). Объём событий мал, поэтому одна общая
// очередь, а не по email.
var marketingMu sync.Mutex

// marketingContact шлёт контакт с полями в фоне. fields может быть nil —
// тогда контакт просто попадает в список.
func (s *Server) marketingContact(email string, fields map[string]string) {
	c := s.unisender()
	if !c.Enabled() || strings.TrimSpace(email) == "" {
		return
	}
	go func() {
		marketingMu.Lock()
		defer marketingMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.Subscribe(ctx, email, fields); err != nil {
			log.Printf("[marketing] email=%s fields=%v err=%v", email, fields, err)
			return
		}
		log.Printf("[marketing] email=%s fields=%v ok", email, fields)
	}()
}

// marketingOrder — то же по email покупателя заказа.
func (s *Server) marketingOrder(orderID string, fields map[string]string) {
	if !s.unisender().Enabled() || s.isPartnerOrder(orderID) {
		return
	}
	s.marketingContact(extractCustomerEmail(orderID, s.DB), fields)
}

// События API-триггера (UNISENDER_TRIGGER_URL). Поля-чекбоксы выше не
// различают два заказа одного контакта: второй `order_pay=1` поле не меняет,
// и цепочка не стартует. Событие уходит на каждый заказ отдельно.
const (
	EventOrderCreated = "order_created" // заказ создан (есть email), оплаты нет
	EventOrderPaid    = "order_paid"    // первая оплата / песня по подписке
	EventSongReady    = "song_ready"    // письмо «Ваша песня готова» ушло
	EventSongListened = "song_listened" // «Прослушать» на оплаченном заказе
	EventSongDownload = "song_download" // «Скачать MP3» на оплаченном заказе
)

// marketingEvent шлёт событие заказа в API-триггер. event_id = order_id +
// event_type: одно и то же событие по заказу всегда получает один id, по нему
// сценарий отсекает повтор вебхука. Ссылки — те же, что в письмах сайта:
// checkout_url «продолжить оформление» есть только пока заказ не оплачен.
func (s *Server) marketingEvent(orderID, eventType string) {
	if strings.TrimSpace(s.Config.UnisenderTriggerURL) == "" {
		return
	}
	o, err := store.GetOrder(context.Background(), s.DB, orderID)
	if err != nil {
		log.Printf("[marketing] event=%s order=%s get order err=%v", eventType, orderID, err)
		return
	}
	// Покупатель партнёра (Les Roses) нам на рассылки не подписывался.
	if partnerOf(o) != "" {
		return
	}
	email := strings.ToLower(extractCustomerEmail(orderID, s.DB))
	if email == "" {
		return
	}
	ref := publicOrderRef(s.Config.WebhookSecret, orderID)
	base := strings.TrimSuffix(s.Config.PublicAppURL, "/")
	payload := map[string]any{
		"email":          email,
		"order_id":       orderID,
		"order_ref":      ref,
		"event_id":       orderID + ":" + eventType,
		"event_type":     eventType,
		"occurred_at":    time.Now().UTC().Format(time.RFC3339),
		"payment_status": o.PaymentStatus,
		"order_status":   o.Status,
		"order_url":      base + "/order/" + ref,
		"status_url":     base + "/api/public/order/" + ref + "/status",
		"checkout_url":   nil,
		"recipient_name": jsonRecipient(o.InputPayloadJSON),
		"flow_arm":       nullStr(armLinkName(o.FlowVariant.String)),
	}
	if resume := s.resumeURL(o); resume != "" {
		payload["checkout_url"] = base + resume
	}
	c := s.unisender()
	go func() {
		marketingMu.Lock()
		defer marketingMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.Trigger(ctx, s.Config.UnisenderTriggerURL, payload); err != nil {
			log.Printf("[marketing] event=%s order=%s email=%s err=%v", eventType, orderID, email, err)
			return
		}
		log.Printf("[marketing] event=%s order=%s email=%s ok", eventType, orderID, email)
	}()
}

// handleOrderEngagement принимает «Прослушать» / «Скачать» со страницы заказа
// и воронки /create. Ответ всегда 200: это фоновый пинг, как /flow/step.
func (s *Server) handleOrderEngagement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Event string `json:"event"`
	}
	_ = readJSON(r, &body)
	field := ""
	switch body.Event {
	case "listen":
		field = marketing.FieldSongListened
	case "download":
		field = marketing.FieldSongDownloaded
	default:
		writeJSON(w, 200, map[string]any{"ok": false})
		return
	}
	id, ok := s.resolveOrderID(r.Context(), r.PathValue("id"))
	if !ok {
		writeJSON(w, 200, map[string]any{"ok": false})
		return
	}
	if o, err := store.GetOrder(r.Context(), s.DB, id); err != nil || o.PaymentStatus != store.PaymentPaid {
		// Событие имеет смысл только для оплаченной песни: превью до оплаты
		// цепочку «песня прослушана» запускать не должно.
		writeJSON(w, 200, map[string]any{"ok": false})
		return
	}
	s.marketingOrder(id, map[string]string{field: "1"})
	if field == marketing.FieldSongListened {
		s.marketingEvent(id, EventSongListened)
	} else {
		s.marketingEvent(id, EventSongDownload)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
