// Строка заказа для /admin/orders и её выгрузка в CSV.
//
// Из ТЗ «tech requirements Sep 2026», п. 5: в списке заказов должно быть
// однозначно видно, были ли деньги (разовая покупка, песня по подписке, тест),
// через какого провайдера шла оплата и какая у человека подписка; полный URL
// захода и все поля строки — в выгрузке, чтобы не собирать их вручную.
package httpserver

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mezmo-song-com/internal/store"
)

// Чем оплачен заказ. Песня подписчика — заказ на 0: деньги за неё взяты
// платежом подписки, и «оплачен» в статусе без этой пометки читался как продажа.
const (
	PaymentKindOneTime          = "one_time"          // разовая покупка песни
	PaymentKindSubscriptionSong = "subscription_song" // песня по подписке, по заказу денег нет
	PaymentKindInternal         = "internal"          // тестовый заказ из админки
	PaymentKindFree             = "free"              // цена 0 без подписки (промо)
)

type adminOrderRow struct {
	o                 store.Order
	publicRef         string
	parentRef         string
	email, whatsapp   string
	attribution       map[string]any
	paymentKind       string
	subscriptionPrice int
	subStatus         string
	subPayments       store.SubscriptionPaymentSummary
	landingURL        string
	currency          string
}

func (s *Server) adminOrderRow(ctx context.Context, o store.Order) adminOrderRow {
	row := adminOrderRow{
		o:         o,
		publicRef: publicOrderRef(s.Config.WebhookSecret, o.ID),
		parentRef: refOrEmpty(s.Config.WebhookSecret, o.ParentOrderID),
		currency:  s.siteCurrency(),
	}
	var cust map[string]any
	_ = json.Unmarshal([]byte(o.CustomerJSON), &cust)
	row.email, _ = cust["email"].(string)
	row.whatsapp, _ = cust["whatsappNumber"].(string)
	if o.AttributionJSON.Valid {
		_ = json.Unmarshal([]byte(o.AttributionJSON.String), &row.attribution)
	}
	row.landingURL = s.fullLandingURL(row.attribution)

	switch {
	case o.SubscriptionID.Valid && o.SubscriptionID.String != "":
		row.paymentKind = PaymentKindSubscriptionSong
		// Песня подписчика — заказ на 0: отдаём цену подписки, её статус и
		// платежи, чтобы было видно, платил ли человек вообще.
		if sub, err := store.GetSubscription(ctx, s.DB, o.SubscriptionID.String); err == nil {
			row.subscriptionPrice = subPrice(sub)
			row.subStatus = sub.Status
		}
		if sum, err := store.GetSubscriptionPaymentSummary(ctx, s.DB, o.SubscriptionID.String); err == nil {
			row.subPayments = sum
		}
	case o.DiscountCode.Valid && o.DiscountCode.String == "INTERNAL":
		row.paymentKind = PaymentKindInternal
	case orderFinalPrice(o) <= 0:
		row.paymentKind = PaymentKindFree
	default:
		row.paymentKind = PaymentKindOneTime
	}
	return row
}

// fullLandingURL — адрес первого захода целиком. Фронт сохраняет его с
// доменом; у старых меток только путь — домен дописывается из PUBLIC_APP_URL.
func (s *Server) fullLandingURL(attr map[string]any) string {
	u, _ := attr["landingUrl"].(string)
	if strings.HasPrefix(u, "/") {
		return strings.TrimSuffix(s.Config.PublicAppURL, "/") + u
	}
	return u
}

func (r adminOrderRow) json() map[string]any {
	o := r.o
	return map[string]any{
		"id":                 o.ID,
		"publicRef":          r.publicRef,
		"subscribed":         o.SubscriptionID.Valid && o.SubscriptionID.String != "",
		"subscriptionPrice":  r.subscriptionPrice,
		"subscriptionStatus": nullStr(r.subStatus),
		"subscriptionPayments": map[string]any{
			"count":    r.subPayments.Count,
			"firstAt":  nullStr(r.subPayments.FirstAt),
			"lastFlow": nullStr(r.subPayments.LastFlow),
		},
		"paymentKind":       r.paymentKind,
		"paymentProvider":   nullStr(o.PaymentProvider.String),
		"currency":          r.currency,
		"landingUrl":        nullStr(r.landingURL),
		"createdAt":         o.CreatedAt.UTC().Format(time.RFC3339Nano),
		"status":            o.Status,
		"paymentStatus":     o.PaymentStatus,
		"yooPaymentId":      nullStr(o.YooPaymentID.String),
		"deliveryStatus":    o.DeliveryStatus,
		"recipientName":     jsonRecipient(o.InputPayloadJSON),
		"email":             nullStr(r.email),
		"whatsappNumber":    nullStr(r.whatsapp),
		"basePrice":         o.BasePrice,
		"discountCode":      nullStr(o.DiscountCode.String),
		"demoCompleted":     o.DemoCompleted,
		"isFallback":        store.IsFallbackAudio(o.AudioURLsJSON),
		"purchaseTrackedAt": nullStr(o.PurchaseTrackedAt.String),
		"themeSlug":         nullStr(o.ThemeSlug.String),
		"attribution":       r.attribution,
		// Плечо теста и родитель апсейла: без них заказ ценового плеча и заказ
		// на стемы в списке неотличимы от обычных.
		"flowVariant":  nullStr(o.FlowVariant.String),
		"flow":         nullStr(o.Flow.String),
		"funnelStage":  nullStr(o.FunnelStage.String),
		"parentRef":    nullStr(r.parentRef),
		"upsellKind":   nullStr(o.UpsellKind.String),
		"deliverAfter": nullStr(o.DeliverAfter.String),
	}
}

// csvColumns — шапка выгрузки. Порядок фиксирован: по нему же собирается строка.
var csvColumns = []string{
	"created_at", "order_id", "order_url", "email", "phone", "recipient",
	"arm", "flow", "price", "currency", "payment_status", "payment_kind", "payment_provider", "provider_payment_id",
	"subscription_status", "subscription_price", "subscription_payments", "subscription_last_payment",
	"song_status", "delivery_status",
	"landing_url", "referrer", "utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term", "ref",
	"gclid", "fbclid", "yclid",
}

func (r adminOrderRow) csv(orderURL string) []string {
	o := r.o
	attr := func(k string) string { v, _ := r.attribution[k].(string); return v }
	subPrice := ""
	if r.subscriptionPrice > 0 {
		subPrice = strconv.Itoa(r.subscriptionPrice)
	}
	return []string{
		o.CreatedAt.UTC().Format(time.RFC3339), o.ID, orderURL, r.email, r.whatsapp, jsonRecipient(o.InputPayloadJSON),
		o.FlowVariant.String, o.Flow.String, strconv.Itoa(orderFinalPrice(o)), r.currency, o.PaymentStatus, r.paymentKind, o.PaymentProvider.String, o.YooPaymentID.String,
		r.subStatus, subPrice, strconv.Itoa(r.subPayments.Count), r.subPayments.LastFlow,
		o.Status, o.DeliveryStatus,
		r.landingURL, attr("referrer"), attr("utm_source"), attr("utm_medium"), attr("utm_campaign"), attr("utm_content"), attr("utm_term"), attr("ref"),
		attr("gclid"), attr("fbclid"), attr("yclid"),
	}
}

// csvExportLimit — потолок строк одной выгрузки: защищает 1 ГБ памяти сервера.
const csvExportLimit = 20000

// handleAdminOrdersCSV — выгрузка списка заказов с теми же q и filter, что у
// /api/admin/orders, но без страниц: все совпавшие заказы одним файлом.
func (s *Server) handleAdminOrdersCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders-`+time.Now().UTC().Format("2006-01-02")+`.csv"`)
	// BOM — чтобы Excel открыл UTF-8 с кириллицей без ручного выбора кодировки.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	_ = cw.Write(csvColumns)
	const batch = 500
	for offset := 0; offset < csvExportLimit; offset += batch {
		orders, _, err := store.ListOrdersPage(ctx, s.DB, store.OrderPage{
			Query:  r.URL.Query().Get("q"),
			Filter: r.URL.Query().Get("filter"),
			Limit:  batch,
			Offset: offset,
		})
		if err != nil {
			log.Printf("[admin] orders csv: %v", err)
			break
		}
		for _, o := range orders {
			_ = cw.Write(s.adminOrderRow(ctx, o).csv(s.publicOrderURL(o.ID)))
		}
		if len(orders) < batch {
			break
		}
	}
	cw.Flush()
}
