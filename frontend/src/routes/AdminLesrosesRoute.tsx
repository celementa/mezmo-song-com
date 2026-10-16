import { useCallback, useEffect, useState } from 'react'
import { Copy, Download, RefreshCw } from 'lucide-react'
import { AdminNav } from '@/components/admin/AdminNav'

// Кабинет партнёра Les Roses: песни, проданные допником в корзине их магазина.
// Видят его и наш admin (через навигацию), и сам партнёр — отдельной ролью
// lesroses, которой открыта только эта страница (cmd/server/adminauth.go).
// Источник правды о деньгах — InSales; здесь — что стало с каждой песней и
// воронка A/B «блок в корзине / попап перед оформлением».

type ArmStats = {
  arm: string
  shown: number
  added: number
  declined: number
  paid: number
  briefed: number
  ready: number
  canceled: number
  revenue: number
}

type PartnerOrder = {
  id: string
  publicRef: string
  url: string
  number: string
  insalesId: string
  email: string
  arm: string
  state: 'awaiting_brief' | 'generating' | 'ready' | 'canceled'
  recipient: string
  reminders: number
  canceledAt: string | null
  createdAt: string
  brief: { openedAt: string | null; step: number; stepAt: string | null; steps: number }
  mail: {
    sent: number
    tracked: boolean
    delivered: boolean
    problem: string
    openedAt: string | null
    openedHumanAt: string | null
  }
}

type Data = { orders: PartnerOrder[]; funnel: ArmStats[]; days: number; insalesConfigured: boolean }

const ARM_TITLE: Record<string, string> = {
  cart: 'LesRoses — корзина',
  popup: 'LesRoses — попап',
  manual: 'Вручную',
}

const STATE: Record<PartnerOrder['state'], { label: string; cls: string }> = {
  awaiting_brief: { label: 'ждём анкету', cls: 'bg-amber-50 text-amber-700' },
  generating: { label: 'песня создаётся', cls: 'bg-blue-50 text-blue-700' },
  ready: { label: 'песня готова', cls: 'bg-green-50 text-green-700' },
  canceled: { label: 'отменён', cls: 'bg-gray-100 text-gray-500' },
}

const PERIODS = [7, 30, 90, 365]

function pct(part: number, whole: number) {
  if (!whole) return '—'
  return `${Math.round((part / whole) * 1000) / 10}%`
}

function fmtDate(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}

const rub = (n: number) => `${n.toLocaleString('ru-RU')} ₽`

export function AdminLesrosesRoute() {
  const [days, setDays] = useState(30)
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [data, setData] = useState<Data | null>(null)
  const [isAdmin, setIsAdmin] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(() => {
    const params = new URLSearchParams({ days: String(days) })
    if (search) params.set('q', search)
    fetch(`/api/admin/lesroses/orders?${params}`, { headers: { Accept: 'application/json' } })
      .then((res) => {
        setIsAdmin(res.headers.get('X-Admin-Role') === 'admin')
        return res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))
      })
      .then((d: Data) => {
        setData(d)
        setError(null)
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Ошибка загрузки'))
      .finally(() => setLoading(false))
  }, [days, search])

  useEffect(load, [load])

  const total = (data?.funnel ?? []).reduce(
    (acc, a) => ({ paid: acc.paid + a.paid, revenue: acc.revenue + a.revenue, ready: acc.ready + a.ready }),
    { paid: 0, revenue: 0, ready: 0 },
  )

  return (
    <div className="min-h-screen bg-gray-50 px-4 py-6">
      <div className="mx-auto max-w-6xl space-y-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold text-gray-900">Les Roses × Mezmo</h1>
            <p className="text-sm text-gray-500">Персональные песни, проданные к букетам на lesrosesdechantal.ru</p>
          </div>
          {isAdmin ? (
            <AdminNav current="/admin/lesroses" />
          ) : (
            <a href="/admin/logout" className="text-sm text-gray-500 hover:text-gray-800">
              Выйти
            </a>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="flex overflow-hidden rounded-lg border border-gray-300 bg-white">
            {PERIODS.map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => setDays(d)}
                className={`px-3 py-2 text-sm font-medium ${d === days ? 'bg-gray-900 text-white' : 'text-gray-600 hover:bg-gray-100'}`}
              >
                {d === 365 ? 'Год' : `${d} дн.`}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => {
              setLoading(true)
              load()
            }}
            className="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 hover:bg-gray-100"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} /> Обновить
          </button>
          <a
            href={`/api/admin/lesroses/orders.csv?days=${days}`}
            className="inline-flex items-center gap-1.5 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 hover:bg-gray-100"
          >
            <Download className="h-4 w-4" /> CSV для сверки
          </a>
        </div>

        {error && <p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{error}</p>}
        {data && !data.insalesConfigured && isAdmin && (
          <p className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800">
            Ключ InSales не задан (INSALES_*): вебхук и сверка выключены, заказы заводятся только вручную.
          </p>
        )}

        <div className="grid gap-3 sm:grid-cols-3">
          <Stat label="Оплачено песен" value={String(total.paid)} />
          <Stat label="Готово песен" value={String(total.ready)} />
          <Stat label="Сумма продаж (без отменённых)" value={rub(total.revenue)} />
        </div>

        <section className="overflow-x-auto rounded-xl border border-gray-200 bg-white">
          <h2 className="border-b border-gray-100 px-4 py-3 text-sm font-semibold text-gray-800">Воронка по вариантам показа</h2>
          <table className="w-full min-w-[720px] text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-4 py-2">Вариант</th>
                <th className="px-4 py-2 text-right">Показы</th>
                <th className="px-4 py-2 text-right">Добавили</th>
                <th className="px-4 py-2 text-right" title="Закрыли попап: «Нет, спасибо», крестик или клик мимо окна">
                  Закрыли
                </th>
                <th className="px-4 py-2 text-right">Оплатили</th>
                <th className="px-4 py-2 text-right">Конверсия</th>
                <th className="px-4 py-2 text-right">Анкета</th>
                <th className="px-4 py-2 text-right">Готово</th>
                <th className="px-4 py-2 text-right">Отмены</th>
              </tr>
            </thead>
            <tbody>
              {(data?.funnel ?? []).length === 0 && (
                <tr>
                  <td colSpan={9} className="px-4 py-6 text-center text-gray-400">
                    За период данных нет
                  </td>
                </tr>
              )}
              {(data?.funnel ?? []).map((a) => (
                <tr key={a.arm} className="border-t border-gray-100">
                  <td className="px-4 py-2 font-medium text-gray-800">{ARM_TITLE[a.arm] ?? a.arm}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{a.arm === 'manual' ? '—' : a.shown}</td>
                  <td className="px-4 py-2 text-right tabular-nums">
                    {a.arm === 'manual' ? '—' : `${a.added} (${pct(a.added, a.shown)})`}
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">
                    {a.arm === 'popup' ? `${a.declined} (${pct(a.declined, a.shown)})` : '—'}
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">{a.paid}</td>
                  <td className="px-4 py-2 text-right tabular-nums" title="Оплатили от показов">
                    {a.arm === 'manual' ? '—' : pct(a.paid, a.shown)}
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">{a.briefed}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{a.ready}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{a.canceled}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="px-4 py-2 text-xs text-gray-400">
            Показы, добавления и закрытия — люди за день (сессии браузера), оплаты — заказы InSales со строкой песни.
            Закрыть можно только попап, у блока в корзине кнопки «Нет» нет.
          </p>
        </section>

        <ManualOrder onCreated={load} />

        <section className="overflow-x-auto rounded-xl border border-gray-200 bg-white">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-4 py-3">
            <h2 className="text-sm font-semibold text-gray-800">Заказы ({data?.orders.length ?? 0})</h2>
            <form
              onSubmit={(e) => {
                e.preventDefault()
                setSearch(query.trim())
              }}
            >
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="№ заказа или email"
                className="w-56 rounded-lg border border-gray-300 px-3 py-1.5 text-sm"
              />
            </form>
          </div>
          <table className="w-full min-w-[940px] text-sm">
            <thead className="bg-gray-50 text-left text-xs uppercase tracking-wide text-gray-500">
              <tr>
                <th className="px-4 py-2">Дата</th>
                <th className="px-4 py-2">№ Les Roses</th>
                <th className="px-4 py-2">Email</th>
                <th className="px-4 py-2">Вариант</th>
                <th className="px-4 py-2">Письмо</th>
                <th className="px-4 py-2">Песня</th>
                <th className="px-4 py-2">Ссылка покупателя</th>
              </tr>
            </thead>
            <tbody>
              {(data?.orders ?? []).map((o) => (
                <tr key={o.id} className="border-t border-gray-100 align-top">
                  <td className="whitespace-nowrap px-4 py-2 text-gray-600">{fmtDate(o.createdAt)}</td>
                  <td className="px-4 py-2 font-medium text-gray-900">{o.number}</td>
                  <td className="px-4 py-2 text-gray-700">{o.email || <span className="text-red-600">нет email</span>}</td>
                  <td className="px-4 py-2 text-gray-600">{ARM_TITLE[o.arm] ?? o.arm}</td>
                  <td className="px-4 py-2">
                    <MailStatus mail={o.mail} />
                  </td>
                  <td className="px-4 py-2">
                    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${STATE[o.state].cls}`}>{STATE[o.state].label}</span>
                    {o.recipient && <div className="mt-1 text-xs text-gray-500">для: {o.recipient}</div>}
                    {o.state === 'awaiting_brief' && <BriefProgress brief={o.brief} />}
                    {o.state === 'awaiting_brief' && o.reminders > 0 && (
                      <div className="mt-1 text-xs text-gray-400">напоминаний: {o.reminders}</div>
                    )}
                    {o.canceledAt && o.state !== 'canceled' && (
                      <div className="mt-1 text-xs text-amber-700">отменён в InSales после анкеты</div>
                    )}
                  </td>
                  <td className="px-4 py-2">
                    <CopyLink url={o.url} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      </div>
    </div>
  )
}

// Судьба письма со ссылкой на анкету — по вебхуку Unisender Go. «Открыто»
// без пометки «человеком» — почтовик сам загрузил картинки (Apple Mail
// открывает так всё подряд), прочтение это не доказывает.
function MailStatus({ mail }: { mail: PartnerOrder['mail'] }) {
  if (mail.sent === 0) return <span className="text-xs text-gray-400">не отправлено</span>
  if (!mail.tracked) return <span className="text-xs text-gray-400">отправлено, без отслеживания</span>
  if (mail.problem === 'spam') return <span className="text-xs font-medium text-red-600">пожаловались: спам</span>
  if (mail.problem === 'hard_bounced') return <span className="text-xs font-medium text-red-600">не доставлено</span>
  if (mail.openedHumanAt)
    return <span className="text-xs font-medium text-green-700">открыто {fmtDate(mail.openedHumanAt)}</span>
  if (mail.openedAt)
    return (
      <span className="text-xs text-gray-600" title="Почтовик загрузил письмо сам — человек мог его и не читать">
        открыто почтовиком
      </span>
    )
  if (mail.delivered) return <span className="text-xs text-gray-600">доставлено, не открыто</span>
  return <span className="text-xs text-gray-400">в пути</span>
}

function BriefProgress({ brief }: { brief: PartnerOrder['brief'] }) {
  if (!brief.openedAt) return <div className="mt-1 text-xs text-gray-400">ссылку не открывал</div>
  return (
    <div className="mt-1 text-xs text-gray-600">
      открыл {fmtDate(brief.openedAt)}, дошёл до шага {brief.step} из {brief.steps}
      {brief.stepAt && brief.step > 1 && <span className="text-gray-400"> · {fmtDate(brief.stepAt)}</span>}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-gray-200 bg-white px-4 py-3">
      <div className="text-xs text-gray-500">{label}</div>
      <div className="mt-1 text-xl font-semibold tabular-nums text-gray-900">{value}</div>
    </div>
  )
}

function CopyLink({ url }: { url: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard?.writeText(url).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })
      }}
      className="inline-flex items-center gap-1 text-xs text-gray-600 hover:text-gray-900"
      title={url}
    >
      <Copy className="h-3.5 w-3.5" /> {copied ? 'скопировано' : 'копировать'}
    </button>
  )
}

const MANUAL_ERRORS: Record<string, string> = {
  bad_number: 'Номер заказа — только цифры, как в InSales.',
  bad_email: 'Проверьте email покупателя.',
  exists: 'По этому номеру песня уже заведена — ссылка в таблице ниже.',
}

function ManualOrder({ onCreated }: { onCreated: () => void }) {
  const [number, setNumber] = useState('')
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null)

  const submit = async () => {
    setBusy(true)
    setMessage(null)
    try {
      const res = await fetch('/api/admin/lesroses/orders', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ number, email }),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) {
        setMessage({ ok: false, text: MANUAL_ERRORS[body.error] ?? `Ошибка (${res.status})` })
        return
      }
      setMessage({ ok: true, text: `Готово: покупателю ушло письмо со ссылкой на анкету.` })
      setNumber('')
      setEmail('')
      onCreated()
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="rounded-xl border border-gray-200 bg-white px-4 py-4">
      <h2 className="text-sm font-semibold text-gray-800">Завести песню вручную</h2>
      <p className="mt-1 text-xs text-gray-500">
        Если песню продали по телефону или письмо не пришло. Покупатель получит ссылку на анкету; повторно по тому же
        номеру песня не создаётся.
      </p>
      <form
        className="mt-3 flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <input
          value={number}
          onChange={(e) => setNumber(e.target.value)}
          placeholder="№ заказа InSales"
          inputMode="numeric"
          className="w-40 rounded-lg border border-gray-300 px-3 py-2 text-sm"
          required
        />
        <input
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="email покупателя"
          type="email"
          className="w-64 rounded-lg border border-gray-300 px-3 py-2 text-sm"
          required
        />
        <button
          type="submit"
          disabled={busy}
          className="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-60"
        >
          {busy ? 'Создаём…' : 'Создать и отправить ссылку'}
        </button>
      </form>
      {message && <p className={`mt-2 text-sm ${message.ok ? 'text-green-700' : 'text-red-700'}`}>{message.text}</p>}
    </section>
  )
}
