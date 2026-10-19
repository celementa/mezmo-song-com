# mezmo-song.com

Сайт сервиса персональных песен для международной аудитории. Юрлицо —
**Celementa FZCO** (Dubai Silicon Oasis, ОАЭ), цены в долларах с
переключателем «$ / ₽» для покупателей из России, оплата — **UnitPay**.

Код — перенос из [ai-songs](https://github.com/gr1ffonner/ai-songs) (коммит
`a2d3ad2`): фронт (`frontend/`, `packages/shared/`) и Go-бэкенд (`cmd/`,
`internal/`, `data/prompts/`); всё, что отличается от mezmo.ru, — правки поверх.

## Команды

```bash
npm install
npm run dev        # Vite на :5173, /api проксируется на BACKEND_PORT (8080)
npm run build      # frontend/dist
npm run check      # проверка бандла перед выкладкой
npm run typecheck
go test ./internal/...
go run ./cmd/server   # бэкенд; без UNITPAY_* заказы создаются сразу оплаченными (mock)
```

## Воронки и плечи

| Плечо (`?arm=`) | Воронка | Цена |
|---|---|---|
| `variant3-990` | `/config`: анкета → оплата | $25 |
| `variant3-500-750s` | `/config`: анкета → оплата | разовая $19 (подписка $15 — после включения подписок) |
| `variant4-990` | `/create?v=2`: email → оплата → анкета | $25 |
| `variant4-500-750s` | `/create?v=2`: email → оплата → анкета | разовая $19 |

Плечи и их цены включаются на `/admin/settings` (по умолчанию в ротации только
`variant3-990`). Определения — `internal/httpserver/flow.go`. В имени плеча —
рублёвые цены mezmo.ru (500 → $15, 750 → $19, 990 → $25), так их называет реклама.

**Подписок нет**, пока UnitPay не включит рекуррентные платежи (их согласует
служба безопасности UnitPay): экран сравнения и апсейл подписки скрыты, ручки
оформления отвечают `503 subscriptions_unavailable` (`requireSubscriptions`).

**Prepay (`variant4`):** после оплаты UnitPay возвращает на `/create?order=…&paid=1`
— воронка встаёт на первый вопрос анкеты; одновременно уходит письмо «Оплата
получена» со ссылкой на продолжение (`needs_story`). Генерация ждёт анкету
(`POST /api/orders/{id}/config`).

## Оплата: UnitPay

`internal/payment/unitpay.go` (клиент API), `internal/httpserver/unitpay.go`
(выставление счёта, уведомления, сверка).

- Счёт — `initPayment` по кнопке на шаге оплаты, после обязательной галочки
  оферты; покупатель уходит на форму UnitPay. Повторный заход в течение 50 минут
  получает тот же счёт (две открытые формы — две оплаты одной песни).
- Уведомления — `GET /api/webhooks/unitpay` (`check` / `pay` / `error`):
  проверяются IP UnitPay, подпись, сумма и валюта. Ошибочный ответ на `pay`
  помечает платёж «Не завершён», повтор — из кабинета UnitPay.
- Потерянное уведомление подбирает фоновая сверка (`getPayment` раз в 5 минут).
- Двойная оплата не возвращается автоматически — в логе `[pay] unitpay dup`,
  возврат вручную в кабинете.
- **Тестовый режим** (`UNITPAY_TEST=1`): UnitPay принимает только RUB и карту,
  поэтому сумма пересчитывается по `UNITPAY_TEST_RUB_RATE` ($25 → 2 138 ₽).
  Тестовые платежи UnitPay не сохраняет (id после оплаты меняется); приходят ли
  по ним уведомления, документация не говорит — путь уведомлений покрыт тестами
  (`unitpay_test.go`), а `settleUnitPayPayment` принимает и незнакомый id.

**В кабинете UnitPay** (настройки проекта): URL обработчика
`https://mezmo-song.com/api/webhooks/unitpay`, Success/Fail URL —
`https://mezmo-song.com/`.

## Выкладка

Пуш в `main` → GitHub Actions (`.github/workflows/deploy.yml`): сборка фронта,
тесты и сборка бэкенда, проверка бандла, **сначала бэкенд**, потом статика, смоук.

- **Статика** — rsync в `/var/www/mezmo-song.com` ключом `DEPLOY_SSH_KEY`
  (пользователь `deploy-intl`, `rrsync /var/www/mezmo-song.com`).
- **Бэкенд** — бинарь и промпты в `/opt/mezmo-com/incoming` ключом
  `DEPLOY_API_SSH_KEY` (тот же пользователь, `rrsync /opt/mezmo-com/incoming`);
  файл `READY` запускает `mezmo-com-install.path` → `install-release.sh` ставит
  бинарь и перезапускает `mezmo-com.service`. CI ждёт, пока `/health` отдаст
  `X-Build` нового коммита, и только потом выкладывает статику.
- nginx проксирует `/api`, `/admin`, `/health` на `127.0.0.1:8080`
  (`deploy/nginx-api.conf`) и **перезаписывает** `X-Forwarded-For` адресом
  клиента — на нём держится проверка IP UnitPay.
- Ключ хоста — в `DEPLOY_KNOWN_HOSTS`. Правки руками на сервере стираются
  следующей выкладкой — всё меняется только в коде; секреты — в `/opt/mezmo-com/.env`
  (шаблон — `.env.example`).
- Откат — `git revert` и пуш; бэкенд вручную — `bin/server.prev` (см. `install-release.sh`).
- Разовая настройка сервера — `deploy/setup-server.sh`.

Сервер: Ubuntu 20.04, nginx 1.18 из apt (`/etc/nginx/sites-available/mezmo-song.com`,
SPA-фолбэк на `index.html`), Let's Encrypt через certbot с автопродлением.
БД (SQLite) и песни — `/opt/mezmo-com/data`. DNS в GoDaddy: `@` и `www` → `109.248.42.189`.

## Что где

| Что | Где |
|---|---|
| Реквизиты компании, список юридических страниц | `frontend/src/lib/site.ts` |
| Тексты оферты, политик и согласия | `frontend/src/legal/*.md` (рендер — `routes/LegalPage.tsx`) |
| Дизайн и тексты лендинга | `frontend/intl/settings.json` — снимок настроек mezmo.ru, вшивается в `index.html` при сборке; цена плеча из `?arm=` берётся с сервера |
| Валюта показа и курс ₽/$ | `frontend/src/lib/currency.ts` (`RUB_PER_USD`); пересчёт — в `fmtCurrency` (`routes/config/utils.ts`), переключатель — `components/CurrencySwitch.tsx` |
| Воронка `/config` | `frontend/src/flow/FlowRoute.tsx` (вход — `routes/FlowEntry.tsx`) |
| Воронка `/create` (prepay) | `frontend/src/routes/create/` |
| Шаг оплаты (обе воронки) | `frontend/src/features/payment/UnitPayCheckout.tsx` |
| Страница заказа | `frontend/src/routes/OrderDeliveryRoute.tsx` |
| Админка | `/admin/orders` (тип оплаты, провайдер, URL захода, «Скачать CSV»), `/admin/analytics`, `/admin/settings`, `/admin/emails` |
| Футер с реквизитами и логотипами | `frontend/src/templates/gold.tsx`, `components/landing/PaymentLogos.tsx` |

Сайт закрыт от индексации (`noindex`, `robots.txt`).

## Требования банка и UnitPay

| Требование | Где закрыто |
|---|---|
| Реквизиты юрлица | футер, `/contacts`, документы |
| Описание услуги и цена | лендинг (цена у кнопки «Создать песню»), шаг оплаты |
| Цены в рублях для РФ | переключатель «$ / ₽» в шапке и на шаге оплаты; оферта, п. 3.1 |
| Оферта, юрисдикция ОАЭ / суды Дубая | `/terms` |
| Условия возврата, отмены, доставки, оплаты | `/terms`, разделы 3, 5, 6 |
| Политика информационной безопасности | `/security` (приложение 1 к оферте) |
| Политика обработки персональных данных | `/privacy` |
| Согласие на обработку, передачу и хранение ПДн | `/consent` |
| Согласие с офертой при оплате | обязательная галочка на шаге оплаты, без неё кнопка неактивна |
| Логотипы платёжных систем | футер и шаг оплаты |

## Открытые вопросы

- Номер лицензии Celementa FZCO — если отличается от регистрационного, добавить
  в `site.ts` и в `legal/terms.md`.
- Контакт для запросов по персональным данным — сейчас `support@mezmo-song.com`.
- Тексты документов не вычитаны юристом.
- Зачёркнутая цена $75 — подобрана пропорционально mezmo.ru (990 / 2990 ₽).
