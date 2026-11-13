# CLAUDE.md

Короткая памятка для работы в этом репозитории: команды и конвенции. Что где лежит и
как устроены фичи — в `README.md` (воронки, UnitPay, выкладка) и `summary.md`
(подписка ЮKassa). Эти файлы не дублируем.

## Стек

- **Бэкенд:** Go 1.25, stdlib `net/http`, SQLite (`modernc.org/sqlite`, без CGo);
  ни фреймворков, ни ORM.
- **Фронтенд:** React 19 + TypeScript + Vite + Tailwind; npm workspaces `frontend`,
  `packages/shared`. Один Go-бинарь отдаёт собранный SPA.

## Команды

```bash
npm install
npm run dev          # Vite :5173, /api проксируется на BACKEND_PORT (8080)
npm run typecheck    # tsc -b по всем workspaces
npm run build        # shared → frontend/dist
npm run check        # scripts/check-intl-bundle.sh frontend/dist
go run ./cmd/server  # без ключей провайдеров заказы создаются сразу оплаченными (mock)
go test ./...
go vet ./...
npx eslint <файлы>   # запускать из frontend/ и только по изменённым файлам
```

- CI (`.github/workflows/deploy.yml`): `npm ci && npm run build`, `go test
  ./internal/... ./cmd/...`, `npm run check`, затем деплой — **сначала бэкенд**, потом
  статика.
- `npm run lint` по всему репо **красный** из-за 2 pre-existing ошибок
  (`OrderDeliveryRoute.tsx` вызывает `Date.now()` в рендере;
  `react-refresh/only-export-components`). Чужой долг не чинить — скоупить линт
  изменёнными файлами.
- Отдельного раннера фронтовых тестов нет.

## Конвенции

- Комментарии по-русски и объясняют **почему**, а не «что». Публичные функции Go — с
  комментарием.
- HTTP-хендлеры `handleX` — в `internal/httpserver`, состояние и SQL — в
  `internal/store`, бизнес-логика не живёт в `cmd/`.
- Страницы — `frontend/src/routes/<Name>Route.tsx`, доменные компоненты —
  `frontend/src/features/<domain>/`.
- Строки интерфейса — `tr('рус', 'eng')` из `@/lib/lang`. **Исключение:** страницы
  подписки RU-only, в них русские литералы.
- Ссылки на страницы строить через `localePath()` (`@/lib/lang`). В письмах и в
  `return_url` платёжных систем — явный `/ru/...`: инлайн-редирект в
  `frontend/index.html` уводит беспрефиксные адреса на `/en`.
- React Compiler включён: `setState` синхронно в `useEffect` — ошибка
  `react-hooks/set-state-in-effect`, `Date.now()`/`Math.random()` в рендере — ошибка
  `react-hooks/purity`. Асинхронную загрузку запускать через
  `void Promise.resolve().then(load)`.
- Деньги: рубли — целые `int`, копейки — только внутри `internal/payment`.
- Публичные ссылки заказа — подписанный **ref**, сырой `id` наружу не отдаётся.
- Карта подписки привязана только через `subscriptions.yoo_payment_method_id`; ручки
  «отвязать карту, оставив подписку» нет — отвязка происходит при отмене.
- Коммиты и пуш — только по просьбе пользователя. `.orchestrator/` в `.gitignore`.
