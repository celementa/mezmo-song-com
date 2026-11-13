# mezmo-song-com — сводка

Сжатое состояние проекта по факту кода. Длинное описание (воронки, ценовые плечи,
UnitPay, выкладка, требования банка) — в `README.md`; аналитика — `ANALYTICS.md` и
`GRAFANA.md`. Команды и конвенции разработки — в `CLAUDE.md`.

## Подписка ЮKassa (кабинет подписки)

Фича добавила платную подписку на песни на рублёвой ветке ЮKassa: RU-only страницы
оформления, подтверждения оплаты, кабинета подписчика и публичной отмены; вход в
кабинет по email без пароля (cookie `sub_session`); рекуррентные списания по
сохранённой карте с отменой в один клик; отдельный режим воронки, где песня
создаётся без оплаты и списывается подпиской. Цена фиксируется в
`subscriptions.price_rub` до создания платежа, поэтому сумма в ЮKassa и надпись на
странице не расходятся.

### Страницы

Страницы — клиентские маршруты React Router (`frontend/src/router.tsx`), Go их не
регистрирует: работает общий SPA-фолбэк `cmd/server/main.go` (`spa`).

| Путь | Файл | Что делает |
|---|---|---|
| `/subscribe` | `routes/SubscribeRoute.tsx` | оформление: имя + email, две обязательные галочки, встроенный виджет ЮKassa, переход на redirect-форму как фолбэк |
| `/subscribe/return` | `routes/SubscribeReturnRoute.tsx` | после оплаты: опрос `GET /api/subscription/status`, вход по email, переход в `/ru/account` |
| `/account` | `routes/AccountRoute.tsx` | кабинет: вход по email, карточка подписки, список песен, отмена, выход |
| `/account/new` | `routes/AccountNewRoute.tsx` | воронка в режиме подписчика (5 шагов) |
| `/subscription/cancel` | `routes/SubscriptionCancelRoute.tsx` | публичная отмена по токену из письма + запрос ссылки по email |
| `/subscription-offer` | `routes/SubscriptionOfferRoute.tsx` | условия подписки (требование ЮKassa) |

Маршруты объявлены и без префикса, и под `/ru`. В `localizedPages` их намеренно не
кладут, поэтому `/en/subscribe` и остальные EN-адреса подписки — 404: подписка
продаётся только от ООО «МБК».

### API подписки

| Метод и путь | Что делает |
|---|---|
| `POST /api/subscription/start` | создаёт или переиспользует подписку, фиксирует цену, создаёт платёж ЮKassa (встроенный виджет + redirect-фолбэк) |
| `GET /api/subscription/status?sid=` | статус подписки для страницы возврата |
| `POST /api/subscription/session` | вход в кабинет по email (cookie `sub_session`, живёт до `access_until`) |
| `POST /api/subscription/logout` | выход |
| `GET /api/subscription/me` | подписка, доступ, суточный лимит песен, счётчик за сутки |
| `GET /api/subscription/orders` | песни, созданные по подписке |
| `POST /api/subscription/orders/draft` | заказ по подписке без оплаты |
| `POST /api/subscription/cancel-request` | письмо со ссылкой отмены (ответ всегда `{ok:true}`) |
| `GET /api/subscription/cancel-info?t=` / `POST /api/subscription/cancel` | отмена по токену из письма или из кабинета |
| `POST /api/webhooks/yookassa` | уведомления ЮKassa: активация, продление, письма, конверсии |
| `POST /api/orders/{id}/subscribe`, `…/subscribe-instead` | апсейл подписки внутри воронки |
| `POST /api/admin/subscriptions/rebill-now`, `GET …/payments`, `POST …/gift` | админка: досрочное списание, платежи, подарок подписки |

Ручки оформления закрыты гейтом `requireSubscriptions`: без ключей ЮKassa/Stripe при
включённом UnitPay отвечают `503 subscriptions_unavailable`.

### Данные

Схема — `migrateSubscriptions` в `internal/store/subscriptions.go`: таблицы
`subscriptions` (email UNIQUE, `status`, `yoo_payment_method_id`, `access_until`,
`next_charge_at`, `cancel_token`, `price_rub`, `pending_payment_id`, атрибуция),
`subscription_payments` (дедупликация платежей) и `subscription_sessions`. Отдельной
таблицы заказов нет: песня подписки — обычный `orders` со ссылкой
`orders.subscription_id`. Статусы: `pending | active | past_due | canceled`.

### Ключевые правила

- **Отмена** (`store.CancelSubscription`): статус `canceled`, `next_charge_at` и
  `yoo_payment_method_id` стираются — это и есть отвязка карты. `access_until` не
  трогается: оплаченный период доживает. Доступ решает `HasAccess` (`now < access_until`),
  а не статус.
- **Цена**: `orderRef` → цена ценового плеча (`armSubscriptionPriceRub`, с оверрайдом
  из админки через `FlowArmSubPriceRub`); `sid` подписки с тем же email →
  сохранённая цена; иначе 990 ₽ (`subscriptionPriceRub`). Записывается
  `SetSubscriptionPrice` (только для `pending`) до создания платежа, и вебхук
  сверяет сумму с БД.
- **Повторное оформление после отмены**: `UpsertPendingSubscription` возвращает
  неактивную строку в `pending`, чистя `next_charge_at`, `yoo_payment_method_id` и
  `pending_payment_id`, но сохраняя `access_until`. Гейт `already_active` (409)
  срабатывает только на `active` и `past_due` с живым доступом.
- **Режим подписчика**: `SUBSCRIBER_STEPS` — 5 шагов (`recipient`, `occasion`,
  `email`, `emotion`, `story`); на последнем шаге `POST /api/subscription/orders/draft`
  создаёт заказ с `PaymentStatus=paid`, `BasePrice=0`, `DiscountCode=SUBSCRIPTION`,
  email берётся из сессии (не из формы), суточный лимит отвечает `429 daily_limit_reached`.
- **Ссылки**: все ссылки подписки — с `/ru`, включая `return_url` платежа и письма
  (`/ru/subscribe`, `/ru/account`, `/ru/subscription/cancel?t=…`). Причина:
  инлайн-редирект в `frontend/index.html` уводит беспрефиксные адреса на `/en` при
  куке `mezmo_lang=en`, а EN-версии подписки нет — человек получил бы 404.

### Ключевые файлы

| Что | Где |
|---|---|
| HTTP-слой подписки: страницы-API, платёж, письма, вебхук, отмена | `internal/httpserver/subscription.go` |
| Оформление из воронки, `createSubscriptionPayment`, апсейл заказа | `internal/httpserver/flow_subscribe.go` |
| Схема, статусы, отмена, ребиллы, сессии, `UpsertPendingSubscription` | `internal/store/subscriptions.go` |
| Сверка потерянных платежей | `internal/httpserver/reconcile.go` |
| Виды писем подписки (`sub_welcome`, `sub_charged`, `sub_cancel_link`, …) | `internal/httpserver/emails.go` + тело писем в `subscription.go` |
| Маршруты страниц | `frontend/src/router.tsx` |
| Страницы | `frontend/src/routes/{SubscribeRoute,SubscribeReturnRoute,AccountRoute,AccountNewRoute,SubscriptionCancelRoute,SubscriptionOfferRoute}.tsx` |
| Общие стили подписки (`formatSubDate`) | `frontend/src/features/subscription/styles.ts` |
| Виджет, возможности оплаты, сессия | `frontend/src/features/payment/{loadYookassaWidget.ts,YookassaCheckout.tsx,capabilities.ts,yookassaSession.ts}` |
| Режим подписчика в воронке | `frontend/src/flow/FlowRoute.tsx`, `frontend/src/routes/FlowEntry.tsx` |

### Как проверять

```bash
npm run typecheck    # tsc -b по workspaces
npm run build        # shared + frontend/dist
npm run check        # scripts/check-intl-bundle.sh frontend/dist
go test ./...
go vet ./...
npx eslint <изменённые файлы>   # из frontend/: репо-wide lint красный из-за pre-existing ошибок
```

- Go-тесты подписки: `internal/httpserver/subscription_test.go` (вебхук, кабинет,
  отмена по токену, цена плеча, повторное оформление) и
  `internal/store/subscriptions_test.go`.
- Фронтового раннера тестов нет. Ручной смоук: оформление на `/ru/subscribe` тестовой
  картой ЮKassa → вход на `/ru/account` → отмена (кнопкой и по ссылке из письма) →
  повторное оформление. Локальный Playwright-скрипт этого смоука лежит в
  `.orchestrator/reports/`; каталог в `.gitignore` и в репозиторий не входит.

## Расхождения с README

В `README.md` (раздел «Воронки и плечи») сказано «**Подписок нет**». Это верно только
для ветки UnitPay: её ручки оформления по-прежнему отвечают `503
subscriptions_unavailable`. На ветке ЮKassa подписка реализована и продаётся — см.
раздел выше. README не переписан: правка раздела остаётся за владельцем документа.
