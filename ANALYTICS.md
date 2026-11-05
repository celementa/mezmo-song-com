# Аналитика mezmo-song.com

Документ описывает, какие события уходят в рекламные системы, с какой суммой и
валютой, и как одна покупка не считается дважды. Источник истины — код; при
расхождении приводится к коду.

## 1. Одна покупка — один id

Сырой id заказа (`orders.id`, он же `trackingId` в API) — общий
`transaction_id` / `event_id` для всех систем:

| Система | Где | Идентификатор |
|---|---|---|
| GTM/GA4 (браузер) | `frontend/src/features/analytics/purchaseTracking.ts` | `transaction_id = trackingId` |
| Meta Pixel в GTM (браузер) | там же — поле `event_id` в dataLayer | `event_id = trackingId` |
| Пиксельные скрипты админки (Meta/TikTok) | там же | `eventID = trackingId` |
| GA4 Measurement Protocol | `sendGA4Purchase` | `transaction_id = orders.id` |
| Meta Conversions API | `sendMetaConversion` | `event_id = orders.id` |
| Метрика (офлайн-конверсии) | `sendMetrikaConversion` | ClientID из `_ym_uid` |
| VK Ads | `sendVKConversion` | `rb_clickid` из attribution |

Публичный `ref` заказа (подписанный id) в аналитику не попадает: он только для
URL. Иначе браузерное и серверное события получили бы разные id.

## 2. Сумма и валюта

Валюта и сумма берутся из заказа (`orders.currency`, `base_price − скидка +
допники`), а не из настроек сайта:

- RU-ветка, ЮKassa — `RUB`;
- INT-ветка, Stripe — `USD`;
- UnitPay — валюта заказа (`RUB` или `USD`), в тестовом режиме рубли.

`paymentAmount` в `/api/public/order/{ref}/status` уже в валюте заказа. Браузерный
`purchase` в dataLayer и пиксельные скрипты получают `value` и `currency`
оттуда. Серверная шина `sendServerConversions` передаёт ту же пару в GA4,
Метрику и Meta CAPI.

VK Ads не имеет поля валюты и трактует выручку как рубли. Поэтому долларовые
покупки в VK **не отправляются** (`sendVKConversion`), чтобы $19 не записались
как 19 ₽.

## 3. Когда отправляется конверсия

Единственная точка — `applyOrderPaid` (`internal/httpserver/server.go`). Её
вызывают вебхуки всех провайдеров:

| Провайдер | Событие | Заказ становится paid |
|---|---|---|
| ЮKassa | `payment.succeeded` / сверка | `settleSucceededPayment` → `applyOrderPaid` |
| UnitPay | `pay` / `getPayment` | `settleUnitPayPayment` → `applyOrderPaid` |
| Stripe | `checkout.session.completed` | `settleStripeSession` → `applyOrderPaid` |

Перед `applyOrderPaid` провайдер-специфичная проверка перечитывает платёж из API
и сверяет сумму и валюту. Тело вебхука доказательством не считается.

`applyOrderPaid` идемпотентен: повторный вебхук не отправляет вторую конверсию
(гард `MarkServerTracked`).

**Продления подписок — не покупка.** `invoice.paid` Stripe только продлевает
доступ (`ExtendStripeSubscription`), конверсию не шлёт. У ЮKassa ребиллы идут
через `sendSubscriptionConversions` и считаются отдельными платежами подписки —
это историческое поведение .com, не покупка песни.

**Апсейл «безлимит за 0 ₽ сегодня»** после уже оплаченного заказа не создаёт
вторую конверсию: заказ уже paid, `applyOrderPaid` видит дубль и молчит.

## 4. Браузерное событие

`runOrderPurchaseTracking` вызывается со страницы заказа и из воронки `/create`.
Порядок:

1. `POST /api/public/order/{ref}/mark-purchase-tracked` — атомарная заявка;
2. только на `first: true` пушится `purchase` в dataLayer — с `transaction_id`
   и `event_id` (оба = сырой id заказа);
3. пиксельные скрипты админки получают `eventID`, `orderValue`, `currency`.

`event_id` нужен тегу GTM «Meta - purchase»: если включён серверный CAPI
(`META_PIXEL_ID` + `META_ACCESS_TOKEN`), Meta схлопывает браузерное и серверное
события по этому ключу. Само поле тег не читает — в настройках тега Event ID
должна стоять переменная dataLayer `event_id`.

`Lead` («оформление началось») отбивается из обеих воронок: `/config` — при
создании заказа на шаге email, `/create` — при `flow/v1/draft` и `flow/v2/start`.
`reach_style_step` пока шлёт только `/config`.

Так покупка отбивается ровно один раз, даже если страницу открыли в двух
вкладках. Тестовые заказы (`INTERNAL`) не трекаются, пока в `/admin/settings`
не включён «Трекать тестовые оплаты».

## 5. Переменные окружения

```
GA4_MEASUREMENT_ID=
GA4_API_SECRET=
METRIKA_COUNTER_ID=
METRIKA_OAUTH_TOKEN=
VK_PIXEL_ID=
VK_GOAL=purchase
META_PIXEL_ID=
META_ACCESS_TOKEN=
META_TEST_EVENT_CODE=
```

Пустые переменные выключают соответствующий синк. Meta CAPI требует Pixel ID и
access token; test event code — только для отладки.

## 6. Админка и Grafana

- `/admin/orders` и CSV отдают `currency` и `payment_provider` по каждому заказу.
- В Grafana суммы нужно группировать по `orders.currency`: рубли и доллары не
  складываются. Панели выручки без разбивки по валюте считаются некорректными.
- `orders.payment_branch` (`ru`/`intl`) — разрез воронки по выбранному способу
  оплаты.

## 7. Проверка

- `go test ./internal/...` — вебхуки, валюты, подписи Stripe, конверсии.
- `npm run typecheck` — формы и типы событий.
- Живая проверка: Stripe test-карта `4242 4242 4242 4242`, ЮKassa test shop
  1517495 (только карта и ЮMoney; СБП/SberPay/T-Pay доступны лишь на боевом
  магазине).
