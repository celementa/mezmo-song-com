import { useEffect, useState } from 'react'
import { Plus, RefreshCw } from 'lucide-react'
import { AdminNav } from '@/components/admin/AdminNav'

/** Платёж подписки: в ЮKassa это обычный платёж, а в списке заказов его нет —
 * песни подписчиков идут заказами на 0 ₽. Без этого блока сверка «сколько
 * денег пришло» с кабинетом ЮKassa не сходилась. */
type SubscriptionPayment = {
  paymentId: string
  subscriptionId: string
  createdAt: string
  email: string
  amount: number
  flow: string
  subStatus: string
}

const SUB_FLOW: Record<string, string> = {
  subscription_first: 'первый платёж',
  subscription_rebill: 'продление',
}

const SUB_STATUS: Record<string, string> = {
  active: 'активна',
  past_due: 'просрочена',
  pending: 'не оплачена',
  canceled: 'отменена',
}

function SubscriptionPayments() {
  const [data, setData] = useState<{ days: number; total: number; payments: SubscriptionPayment[] } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let cancelled = false
    fetch('/api/admin/subscriptions/payments?days=30', { headers: { Accept: 'application/json' } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = await res.json()
        if (!cancelled) setData(json)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Ошибка загрузки')
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return <div className="mb-4 text-sm text-red-700">Платежи подписок не загрузились: {error}</div>
  }
  const payments = data?.payments ?? []
  return (
    <div className="mb-4 rounded-xl border border-violet-200 bg-violet-50/40 shadow-sm">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between px-4 py-3 text-left text-sm"
      >
        <span className="text-gray-800">
          <span className="font-medium">Платежи подписок за {data?.days ?? 30} дн.:</span>{' '}
          {data ? `${payments.length} на ${data.total.toLocaleString('ru-RU')} ₽` : '…'}
          <span className="ml-2 text-xs text-gray-500">— в списке заказов их нет, песни подписчиков идут по 0 ₽</span>
        </span>
        <span className="text-xs text-gray-500">{open ? 'свернуть' : 'показать'}</span>
      </button>
      {open && (
        <div className="overflow-x-auto border-t border-violet-200">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-white text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-2 font-medium">Когда</th>
                <th className="px-3 py-2 font-medium">Email</th>
                <th className="px-3 py-2 font-medium">Сумма</th>
                <th className="px-3 py-2 font-medium">Тип</th>
                <th className="px-3 py-2 font-medium">Подписка</th>
                <th className="px-3 py-2 font-medium">ID ЮKassa</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 bg-white">
              {payments.map((p) => (
                <tr key={p.paymentId}>
                  <td className="whitespace-nowrap px-3 py-2 text-gray-600">{fmtDate(p.createdAt)}</td>
                  <td className="px-3 py-2 text-gray-800">{p.email}</td>
                  <td className="whitespace-nowrap px-3 py-2 text-gray-800">{p.amount} ₽</td>
                  <td className="px-3 py-2 text-gray-600">{SUB_FLOW[p.flow] ?? p.flow}</td>
                  <td className="px-3 py-2 text-gray-600">{SUB_STATUS[p.subStatus] ?? p.subStatus}</td>
                  <td className="px-3 py-2"><YooPaymentId id={p.paymentId} /></td>
                </tr>
              ))}
              {payments.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-3 py-6 text-center text-gray-400">Платежей подписок за период нет</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

type AdminOrder = {
  id: string
  publicRef: string
  createdAt: string
  status: string
  paymentStatus: string
  yooPaymentId: string | null
  deliveryStatus: string
  recipientName: string
  email: string | null
  whatsappNumber: string | null
  basePrice: number
  discountCode: string | null
  subscribed: boolean
  subscriptionPrice: number
  demoCompleted: boolean
  isFallback: boolean
  purchaseTrackedAt: string | null
  themeSlug: string | null
  attribution: Record<string, string> | null
  flowVariant: string | null
  flow: string | null
  funnelStage: string | null
  upsellKind: string | null
  parentRef: string | null
}


/**
 * Источник заказа одной строкой — приоритет как в Go (`attributionSource`):
 * ref → utm_source → домен реферера → «прямой».
 */
function attrSource(attr: Record<string, string> | null): string {
  if (!attr) return '—'
  if (attr.ref) return `ref:${attr.ref}`
  if (attr.utm_source) return attr.utm_source
  if (attr.referrer) {
    try {
      return new URL(attr.referrer).hostname.replace(/^www\./, '')
    } catch {
      return attr.referrer
    }
  }
  return 'прямой'
}

/** Полная расшифровка метки — в title, чтобы не раздувать таблицу. */
function attrDetails(attr: Record<string, string> | null): string {
  if (!attr) return ''
  return Object.entries(attr)
    .map(([k, v]) => `${k}: ${v}`)
    .join('\n')
}

function fmtDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}

type StatusMeta = { text: string; cls: string; hint: string }

const GRAY = 'bg-gray-100 text-gray-600'

// Статус генерации заказа (orders.status).
const ORDER_STATUS: Record<string, StatusMeta> = {
  created: { text: 'создан', cls: GRAY, hint: 'Заказ оформлен, генерация ещё не запускалась' },
  processing: { text: 'генерится', cls: 'bg-blue-100 text-blue-700', hint: 'Пишем текст и музыку — обычно несколько минут' },
  completed: { text: 'готов', cls: 'bg-green-100 text-green-700', hint: 'Генерация завершена, песня сохранена' },
  failed: { text: 'ошибка', cls: 'bg-red-100 text-red-700', hint: 'Генерация упала. Нужно перезапустить заказ вручную' },
}

// Статус оплаты (orders.payment_status).
const PAYMENT_STATUS: Record<string, StatusMeta> = {
  paid: { text: 'оплачен', cls: 'bg-green-100 text-green-700', hint: 'ЮKassa подтвердила платёж' },
  pending: { text: 'не оплачен', cls: 'bg-amber-100 text-amber-700', hint: 'Счёт выставлен, деньги не пришли. Обычно это брошенная корзина' },
  canceled: { text: 'отменён', cls: GRAY, hint: 'Платёж не состоялся: ЮKassa сообщила payment.canceled. Денег не было' },
  refunded: { text: 'возврат', cls: 'bg-red-100 text-red-700', hint: 'Деньги возвращены клиенту. Песня и доработки для него закрыты' },
}

// Как далеко песня дошла до клиента (orders.delivery_status, см. internal/store/store.go).
const DELIVERY_STATUS: Record<string, StatusMeta> = {
  delivery_pending: { text: 'песни нет', cls: GRAY, hint: 'Доставлять пока нечего: заказ создан или ещё генерится' },
  delivery_ready: { text: 'на странице', cls: 'bg-amber-100 text-amber-700', hint: 'Песня открывается на странице заказа, но отправка письма не подтверждена' },
  delivered: { text: 'письмо ушло', cls: 'bg-green-100 text-green-700', hint: 'Письмо «Ваша песня готова» отправлено клиенту' },
}

// Чем закрыт заказ: настоящей песней или демо-треком-заглушкой.
const SONG_KIND: Record<'real' | 'fallback' | 'none', StatusMeta> = {
  real: { text: 'настоящая', cls: GRAY, hint: 'Сгенерированная песня клиента, лежит в нашем хранилище' },
  fallback: { text: 'заглушка', cls: 'bg-red-100 text-red-700', hint: 'Генерация не удалась — заказ закрыт демо-треком. Клиент получил не свою песню' },
  none: { text: '—', cls: 'bg-gray-50 text-gray-400', hint: 'Заказ ещё не завершён — песни нет' },
}

function metaFor(dict: Record<string, StatusMeta>, value: string): StatusMeta {
  return dict[value] ?? { text: value, cls: GRAY, hint: `Неизвестное значение: ${value}` }
}

/**
 * Откуда заказ внутри сайта: плечо A/B-теста, воронка /create или старый
 * чекаут. Плечо точнее воронки (v2_base и v2_prepay сами ведут в /create),
 * поэтому смотрим сначала на него.
 */
const FLOW_LABEL: Record<string, StatusMeta> = {
  v1_base: { text: 'variant1-990', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: 13 шагов, превью до оплаты, базовая цена 990 ₽' },
  v1_price: { text: 'variant2-750s-990', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: 13 шагов, превью до оплаты, разовая 990 ₽ против подписки 750 ₽/мес, на сравнении предвыбрана подписка' },
  v1_price_onetime: { text: 'variant2-750-990s', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: 13 шагов, превью до оплаты, разовая 990 ₽ против подписки 750 ₽/мес, на сравнении предвыбрана разовая' },
  v1_750: { text: 'variant1-750', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: вариант 1, цена 750 ₽, подписка потом 750 ₽/мес' },
  v1_500: { text: 'variant1-500', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: вариант 1, цена 500 ₽, подписка потом 500 ₽/мес' },
  v1_500_750s: { text: 'variant2-500-750s', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: подписка 500 ₽/мес против разовой 750 ₽, на сравнении предвыбрана разовая' },
  v1_500s_750: { text: 'variant2-500s-750', cls: 'bg-indigo-100 text-indigo-700', hint: 'Плечо A/B: подписка 500 ₽/мес против разовой 750 ₽, на сравнении предвыбрана подписка' },
  v2_base: { text: 'v2_late', cls: 'bg-purple-100 text-purple-700', hint: 'Плечо A/B: воронка /create v1 — песня генерируется и слушается ДО оплаты' },
  v2_prepay: { text: 'v2_prepay', cls: 'bg-purple-100 text-purple-700', hint: 'Плечо A/B: воронка /create v2 — email и оплата в начале, песня после' },
  v1: { text: '/create v1', cls: 'bg-purple-100 text-purple-700', hint: 'Воронка /create v1 без плеча (открыта по ?v=1) — песня до оплаты' },
  v2: { text: '/create v2', cls: 'bg-purple-100 text-purple-700', hint: 'Воронка /create v2 без плеча (открыта по ?v=2) — оплата в начале' },
  legacy: { text: 'старый чекаут', cls: GRAY, hint: 'Прежняя воронка: конфигуратор → /checkout → оплата → генерация' },
  stems: { text: 'стемы', cls: 'bg-sky-100 text-sky-700', hint: 'Отдельный заказ-апсейл на стемы к родительской песне' },
}

/** Плечи с экраном сравнения «разовая или подписка»: у оплаченного заказа
 * важно, что выбрал человек, а не что было предвыбрано (суффикс `s` в имени). */
const PRICE_ARMS = new Set(['v1_price', 'v1_price_onetime', 'v1_500_750s', 'v1_500s_750'])

/**
 * Сумма заказа с расшифровкой. Ноль рублей бывает у двух разных вещей —
 * внутреннего тестового заказа и песни подписчика (деньги взяты платежом
 * подписки, см. блок «Платежи подписок»), и путать их нельзя.
 */
function PriceCell({ o }: { o: AdminOrder }) {
  if (o.subscribed || o.discountCode === 'SUBSCRIPTION') {
    return (
      <div>
        <div className="font-medium text-violet-700">по подписке</div>
        {o.subscriptionPrice > 0 && <div className="text-xs text-gray-400">{o.subscriptionPrice} ₽/мес</div>}
      </div>
    )
  }
  if (o.discountCode === 'INTERNAL') {
    return (
      <div>
        <div>{o.basePrice} ₽</div>
        <div className="text-xs text-gray-400">тестовый</div>
      </div>
    )
  }
  const chosenOneTime = o.paymentStatus === 'paid' && o.flowVariant && PRICE_ARMS.has(o.flowVariant)
  return (
    <div>
      <div>{o.basePrice} ₽</div>
      {chosenOneTime && <div className="text-xs text-gray-400">выбрал разовую</div>}
    </div>
  )
}

function flowMeta(o: AdminOrder): StatusMeta {
  if (o.upsellKind) return metaFor(FLOW_LABEL, o.upsellKind)
  return metaFor(FLOW_LABEL, o.flowVariant || o.flow || 'legacy')
}

// Последняя достигнутая стадия воронки (orders.funnel_stage) — только у /create.
const FUNNEL_STAGE: Record<string, string> = {
  started: 'начал',
  email_captured: 'оставил email',
  paid: 'оплатил',
  config_filled: 'ответил на вопросы',
  done: 'песня готова',
}

/**
 * Генерация ДО оплаты (v1-воронки) даёт «готов» у неоплаченного заказа. Чтобы
 * такой заказ не читался как покупка, статус подписывается отдельно.
 */
function orderStatusMeta(o: AdminOrder): StatusMeta {
  if (o.status === 'completed' && o.paymentStatus !== 'paid') {
    return {
      text: 'готов, не куплен',
      cls: 'bg-amber-100 text-amber-700',
      hint: 'Песня сгенерирована до оплаты (бесплатное прослушивание в воронке). Это не покупка: денег не было, полная песня клиенту закрыта',
    }
  }
  return metaFor(ORDER_STATUS, o.status)
}

function deliveryMeta(o: AdminOrder): StatusMeta {
  if (o.deliveryStatus === 'delivery_ready' && o.paymentStatus !== 'paid') {
    return {
      text: 'ждёт оплаты',
      cls: GRAY,
      hint: 'Песня готова, но письмо «Ваша песня готова» уходит только после оплаты',
    }
  }
  return metaFor(DELIVERY_STATUS, o.deliveryStatus)
}

/** Настоящая песня, заглушка или ещё ничего — заказ без контента песни не имеет. */
function songKind(o: AdminOrder): 'real' | 'fallback' | 'none' {
  if (o.isFallback) return 'fallback'
  return o.status === 'completed' ? 'real' : 'none'
}

/**
 * Плашка статуса с расшифровкой: наведение на десктопе, тап на мобилке.
 * Тултип позиционируется fixed по координатам плашки — внутри таблицы его
 * обрезал бы горизонтальный скролл контейнера.
 */
function StatusBadge({ meta }: { meta: StatusMeta }) {
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)

  const show = (el: HTMLElement) => {
    const r = el.getBoundingClientRect()
    setPos({ top: r.bottom + 6, left: Math.max(8, Math.min(r.left, window.innerWidth - 248)) })
  }

  return (
    <>
      <button
        type="button"
        className={`inline-block cursor-help rounded px-2 py-0.5 text-xs font-medium ${meta.cls}`}
        onMouseEnter={(e) => show(e.currentTarget)}
        onMouseLeave={() => setPos(null)}
        onBlur={() => setPos(null)}
        onClick={(e) => (pos ? setPos(null) : show(e.currentTarget))}
      >
        {meta.text}
      </button>
      {pos && (
        <span
          style={{ top: pos.top, left: pos.left }}
          className="pointer-events-none fixed z-50 w-60 rounded-lg bg-gray-900 px-2.5 py-2 text-xs leading-snug text-white shadow-lg"
        >
          {meta.hint}
        </span>
      )}
    </>
  )
}

/**
 * ID платежа в ЮKassa: в таблице показываем укороченным, клик копирует полный —
 * его вставляют в поиск личного кабинета ЮKassa при обработке возвратов.
 */
function YooPaymentId({ id }: { id: string | null }) {
  const [copied, setCopied] = useState(false)
  if (!id) return <span className="text-gray-300">—</span>
  return (
    <button
      type="button"
      title={`${id}\n\nКлик — скопировать`}
      onClick={() => {
        navigator.clipboard.writeText(id).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })
      }}
      className="cursor-pointer font-mono text-xs text-gray-500 hover:text-gray-900"
    >
      {copied ? 'скопировано' : `${id.slice(0, 8)}…`}
    </button>
  )
}

// Фильтры соответствуют константам store.Filter* в Go.
const FILTERS = [
  { value: '', label: 'Все заказы' },
  { value: 'processing', label: 'В работе' },
  { value: 'completed', label: 'Готовые' },
  { value: 'unpaid', label: 'Не оплачены' },
  { value: 'internal', label: 'Тестовые' },
  { value: 'subscription', label: 'По подписке' },
] as const

export function AdminOrdersRoute() {
  const [orders, setOrders] = useState<AdminOrder[]>([])
  const [total, setTotal] = useState(0)
  const [perPage, setPerPage] = useState(50)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // query — то, что набрано в поле; search — то, что уже ушло на сервер.
  // Поиск и фильтрация живут на сервере: страница держит 50 строк из ста тысяч,
  // искать по ней на клиенте было бы поиском по случайной выборке.
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState<string>('')
  const [reloadKey, setReloadKey] = useState(0)

  // Дебаунс: пока набирают, запрос не уходит на каждый символ.
  useEffect(() => {
    const id = setTimeout(() => {
      setSearch(query.trim())
      setPage(1)
    }, 350)
    return () => clearTimeout(id)
  }, [query])

  useEffect(() => {
    setLoading(true)
    setError(null)
    const params = new URLSearchParams({ page: String(page) })
    if (search) params.set('q', search)
    if (filter) params.set('filter', filter)

    let cancelled = false
    fetch(`/api/admin/orders?${params}`, { headers: { Accept: 'application/json' } })
      .then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = await res.json()
        if (cancelled) return
        setOrders(Array.isArray(data.orders) ? data.orders : [])
        setTotal(typeof data.total === 'number' ? data.total : 0)
        if (typeof data.perPage === 'number' && data.perPage > 0) setPerPage(data.perPage)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Ошибка загрузки')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [page, search, filter, reloadKey])

  const load = () => setReloadKey((k) => k + 1)

  const pageCount = Math.max(1, Math.ceil(total / perPage))
  const from = total === 0 ? 0 : (page - 1) * perPage + 1
  const to = Math.min(page * perPage, total)

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="mx-auto max-w-7xl px-4 py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Заказы</h1>
            <p className="text-sm text-gray-500">
              {search || filter ? `Найдено: ${total}` : `Всего: ${total}`}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <AdminNav current="/admin/orders" />
            <a
              href="/admin/ads"
              className="inline-flex items-center gap-1.5 rounded-lg bg-rose-600 px-3 py-2 text-sm font-medium text-white hover:bg-rose-500"
            >
              <Plus className="h-4 w-4" />
              Создать песню
            </a>
            <select
              value={filter}
              onChange={(e) => {
                setFilter(e.target.value)
                setPage(1)
              }}
              className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 focus:border-rose-400 focus:outline-none"
            >
              {FILTERS.map((f) => (
                <option key={f.value} value={f.value}>
                  {f.label}
                </option>
              ))}
            </select>
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Поиск: id, имя, email, телефон, ID ЮKassa"
              className="w-64 rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-rose-400 focus:outline-none"
            />
            <button
              type="button"
              onClick={load}
              className="inline-flex items-center gap-1.5 rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-gray-700"
            >
              <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
              Обновить
            </button>
          </div>
        </div>

        {error && (
          <div className="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
            Не удалось загрузить заказы: {error}
          </div>
        )}

        <SubscriptionPayments />

        <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
          <table className="min-w-full divide-y divide-gray-200 text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-3 py-3 font-medium">Создан</th>
                <th className="px-3 py-3 font-medium">ID</th>
                <th className="px-3 py-3 font-medium">Получатель</th>
                <th className="px-3 py-3 font-medium">Контакты</th>
                <th className="px-3 py-3 font-medium">Откуда</th>
                <th className="px-3 py-3 font-medium">Воронка</th>
                <th className="px-3 py-3 font-medium">Статус</th>
                <th className="px-3 py-3 font-medium">Оплата</th>
                <th className="px-3 py-3 font-medium">ЮKassa</th>
                <th className="px-3 py-3 font-medium">Доставка</th>
                <th className="px-3 py-3 font-medium">Сумма</th>
                <th className="px-3 py-3 font-medium">Песня</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {orders.map((o) => (
                <tr key={o.id} className="hover:bg-gray-50">
                  <td className="whitespace-nowrap px-3 py-3 text-gray-600">{fmtDate(o.createdAt)}</td>
                  <td className="px-3 py-3">
                    <a href={`/order/${o.publicRef}`} className="font-mono text-xs text-rose-600 hover:underline">
                      {o.id}
                    </a>
                  </td>
                  <td className="px-3 py-3 text-gray-800">{o.recipientName || '—'}</td>
                  <td className="px-3 py-3 text-gray-600">
                    <div>{o.email || '—'}</div>
                    {o.whatsappNumber && <div className="text-xs text-gray-400">{o.whatsappNumber}</div>}
                  </td>
                  <td className="px-3 py-3 text-gray-600" title={attrDetails(o.attribution)}>
                    <div>{attrSource(o.attribution)}</div>
                    {o.attribution?.utm_campaign && (
                      <div className="text-xs text-gray-400">{o.attribution.utm_campaign}</div>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-3 py-3">
                    <StatusBadge meta={flowMeta(o)} />
                    {o.funnelStage && (
                      <div className="mt-1 text-xs text-gray-400">{FUNNEL_STAGE[o.funnelStage] ?? o.funnelStage}</div>
                    )}
                  </td>
                  <td className="px-3 py-3"><StatusBadge meta={orderStatusMeta(o)} /></td>
                  <td className="px-3 py-3"><StatusBadge meta={metaFor(PAYMENT_STATUS, o.paymentStatus)} /></td>
                  <td className="whitespace-nowrap px-3 py-3"><YooPaymentId id={o.yooPaymentId} /></td>
                  <td className="px-3 py-3"><StatusBadge meta={deliveryMeta(o)} /></td>
                  <td className="whitespace-nowrap px-3 py-3 text-gray-800"><PriceCell o={o} /></td>
                  <td className="px-3 py-3">
                    <StatusBadge meta={SONG_KIND[songKind(o)]} />
                  </td>
                </tr>
              ))}
              {!loading && orders.length === 0 && (
                <tr>
                  <td colSpan={12} className="px-3 py-10 text-center text-gray-400">
                    {search || filter ? 'Ничего не найдено' : 'Заказов пока нет'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        {total > 0 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <span className="text-sm text-gray-500">
              {from}–{to} из {total}
              {pageCount > 1 && <span className="text-gray-400"> · страница {page} из {pageCount}</span>}
            </span>
            {pageCount > 1 && (
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={page <= 1 || loading}
                  className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-40"
                >
                  ← Назад
                </button>
                <button
                  type="button"
                  onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
                  disabled={page >= pageCount || loading}
                  className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-40"
                >
                  Вперёд →
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
