package httpserver

import (
	"net/http"
	"strings"

	"mezmo-song-com/internal/store"
)

// Ветки оплаты. Выбор ветки — единственное место, где появляется цена: до него
// во флоу нет ни рублей, ни долларов. Валюта и провайдер следуют из ветки, а не
// из языка сайта: на /en можно платить российской картой, на /ru — международной.
const (
	PaymentBranchRU   = "ru"
	PaymentBranchIntl = "intl"

	CurrencyRUB = "RUB"
	CurrencyUSD = "USD"

	ProviderStripe = "stripe"

	// План оплаты: разовая покупка песни или первый месяц подписки вместо неё.
	PaymentPlanOneTime      = "onetime"
	PaymentPlanSubscription = "subscription"
)

// handleSetPaymentBranch фиксирует выбор «Российская карта / Международная
// карта»: ветку, валюту и цену разовой покупки в этой валюте.
//
// Ручка идемпотентна и открыта только для неоплаченного заказа: после денег
// менять валюту и цену нельзя. Повторный выбор с другим значением — это
// «← Назад» и смена ветки до оплаты, поэтому он разрешён.
func (s *Server) handleSetPaymentBranch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.resolveOrderID(ctx, r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}

	var body struct {
		Branch string `json:"branch"`
		Plan   string `json:"plan"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad_json"})
		return
	}
	branch := strings.ToLower(strings.TrimSpace(body.Branch))
	if branch != PaymentBranchRU && branch != PaymentBranchIntl {
		writeJSON(w, 400, map[string]string{"error": "unknown_branch"})
		return
	}
	plan := strings.ToLower(strings.TrimSpace(body.Plan))
	if plan == "" {
		plan = PaymentPlanOneTime
	}
	if plan != PaymentPlanOneTime && plan != PaymentPlanSubscription {
		writeJSON(w, 400, map[string]string{"error": "unknown_plan"})
		return
	}

	o, err := store.GetOrder(ctx, s.DB, id)
	if err == store.ErrNotFound {
		writeJSON(w, 404, map[string]string{"error": "not_found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if o.PaymentStatus != store.PaymentPending {
		writeJSON(w, 409, map[string]string{"error": "not_pending"})
		return
	}
	if !o.FlowVariant.Valid || o.FlowVariant.String == "" {
		writeJSON(w, 400, map[string]string{"error": "no_arm"})
		return
	}
	arm, known := flowArms[o.FlowVariant.String]
	if !known {
		writeJSON(w, 400, map[string]string{"error": "unknown_arm"})
		return
	}

	currency, provider := CurrencyRUB, store.PaymentRUProvider(ctx, s.DB)
	if branch == PaymentBranchIntl {
		currency, provider = CurrencyUSD, store.PaymentIntlProvider(ctx, s.DB)
	}
	// Подписка в этой ветке вообще возможна? Нет — план не фиксируем: иначе
	// заказ остался бы с планом subscription, который нельзя оплатить, и человек
	// узнал бы об этом только 503-й на шаге оплаты.
	if plan == PaymentPlanSubscription && !s.branchSellsSubscriptions(provider) {
		writeJSON(w, 409, map[string]string{"error": "subscriptions_unavailable"})
		return
	}
	// Цена разовой покупки считается всегда, а не только при плане onetime:
	// экран «подписка / разовая» показывает обе цены и после того, как план уже
	// выбран. Брать её из суммы заказа нельзя — там лежит цена ВЫБРАННОГО плана,
	// и после выбора подписки разовая карточка показывала цену подписки.
	oneTime := s.armPriceIn(ctx, arm, currency)
	amount := oneTime
	if plan == PaymentPlanSubscription {
		amount = s.armSubscriptionPriceIn(ctx, arm, currency)
	}
	if amount <= 0 {
		writeJSON(w, 409, map[string]string{"error": "price_unavailable"})
		return
	}

	updated, err := store.SetOrderPaymentChoice(ctx, s.DB, id, branch, currency, plan, amount)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "db"})
		return
	}
	if !updated {
		// Заказ оплатили между чтением и записью — ветку уже не меняем.
		writeJSON(w, 409, map[string]string{"error": "not_pending"})
		return
	}

	writeJSON(w, 200, map[string]any{
		"branch":   branch,
		"currency": currency,
		"plan":     plan,
		"provider": provider,
		"amount":   amount,
		// Обе цены в валюте ветки — для экрана «подписка / разовая».
		// Считает сервер: фронт не пересчитывает курс и не решает, что дороже.
		"oneTimeAmount":      oneTime,
		"subscriptionAmount": s.armSubscriptionPriceIn(ctx, arm, currency),
	})
}
