import { useEffect, useState } from 'react'
import { ChevronDown, Info, Lock, RefreshCw } from 'lucide-react'
import { AdminNav } from '@/components/admin/AdminNav'

// Письма сервера: тумблеры отправки и журнал. Реестр писем живёт в Go
// (internal/httpserver/emails.go) — страница только рисует его, поэтому новое
// письмо появляется здесь само, с подсказками «когда» и «зачем».

type EmailVar = { name: string; desc: string }

type EmailKind = {
  key: string
  group: string
  title: string
  subject: string
  when: string
  why: string
  locked: boolean
  enabled: boolean
  // Шаблон Unisender Go: пусто — письмо уходит нашим текстом.
  templateId: string
  vars: EmailVar[]
}

type EmailsResponse = { kinds: EmailKind[]; commonVars: EmailVar[]; templatesActive: boolean }

type LogItem = {
  id: number
  createdAt: string
  kind: string
  kindTitle: string
  recipient: string
  subject: string
  status: 'sent' | 'failed' | 'disabled'
  error: string
  orderId: string
  orderRef: string
  subscriptionId: string
  // Судьба письма по вебхуку Unisender Go; пусто — письмо ушло не через Go или событий ещё нет.
  delivery: '' | 'delivered' | 'soft_bounced' | 'hard_bounced' | 'spam'
  openedAt: string
  openedHumanAt: string
}

const DELIVERY: Record<Exclude<LogItem['delivery'], ''>, string> = {
  delivered: 'доставлено',
  soft_bounced: 'временная недоставка',
  hard_bounced: 'не доставлено',
  spam: 'жалоба: спам',
}

type LogPage = { items: LogItem[]; total: number; page: number; perPage: number }

/**
 * Подсказка по наведению и по тапу (focus): на телефоне hover нет, а кнопка
 * получает фокус при касании.
 */
function Tooltip({ kind }: { kind: Pick<EmailKind, 'when' | 'why' | 'locked'> }) {
  return (
    <span className="group relative inline-flex">
      <button
        type="button"
        aria-label="Когда и зачем уходит письмо"
        className="rounded-full p-0.5 text-gray-400 hover:text-gray-700 focus:text-gray-700 focus:outline-none"
      >
        <Info className="h-4 w-4" />
      </button>
      <span
        role="tooltip"
        className="pointer-events-none invisible absolute left-1/2 top-full z-20 mt-2 w-72 -translate-x-1/2 rounded-lg bg-gray-900 px-3 py-2.5 text-xs leading-relaxed text-gray-100 opacity-0 shadow-lg transition-opacity group-focus-within:visible group-focus-within:opacity-100 group-hover:visible group-hover:opacity-100 sm:left-0 sm:translate-x-0"
      >
        <span className="block font-semibold text-white">Когда уходит</span>
        <span className="block">{kind.when}</span>
        <span className="mt-2 block font-semibold text-white">Зачем</span>
        <span className="block">{kind.why}</span>
        {kind.locked && (
          <span className="mt-2 block text-amber-300">
            Выключить нельзя: без этого письма ломается сам продукт, а не только рассылка.
          </span>
        )}
      </span>
    </span>
  )
}

function Toggle({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled: boolean }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      disabled={disabled}
      className="inline-flex items-center gap-2 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-100 disabled:opacity-60"
    >
      <span className={`relative inline-block h-5 w-9 rounded-full transition-colors ${checked ? 'bg-rose-600' : 'bg-gray-300'}`}>
        <span
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all ${checked ? 'left-[18px]' : 'left-0.5'}`}
        />
      </span>
      {checked ? 'Отправляется' : 'Выключено'}
    </button>
  )
}

const TEMPLATE_ID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * Шаблон Unisender Go для письма: id из конструктора и подстановки, которые
 * сервер в него передаёт. Тема и текст при заданном шаблоне берутся из него.
 */
function TemplateEditor({
  kind,
  commonVars,
  saving,
  onSave,
}: {
  kind: EmailKind
  commonVars: EmailVar[]
  saving: boolean
  onSave: (templateId: string) => void
}) {
  const [value, setValue] = useState(kind.templateId)
  const trimmed = value.trim()
  const invalid = trimmed !== '' && !TEMPLATE_ID_RE.test(trimmed)
  const changed = trimmed.toLowerCase() !== kind.templateId

  return (
    <div className="mt-3 rounded-lg border border-gray-100 bg-gray-50 p-4">
      <label className="block text-xs font-semibold uppercase tracking-wider text-gray-500" htmlFor={`tpl-${kind.key}`}>
        ID шаблона Unisender Go
      </label>
      <div className="mt-1.5 flex flex-wrap gap-2">
        <input
          id={`tpl-${kind.key}`}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="пусто — уходит наш текст"
          spellCheck={false}
          className={`w-full max-w-sm rounded-lg border px-3 py-2 font-mono text-sm focus:outline-none sm:w-96 ${
            invalid ? 'border-red-400 focus:border-red-500' : 'border-gray-300 focus:border-rose-400'
          }`}
        />
        <button
          type="button"
          disabled={saving || invalid || !changed}
          onClick={() => onSave(trimmed)}
          className="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-40"
        >
          Сохранить
        </button>
        {kind.templateId && (
          <button
            type="button"
            disabled={saving}
            onClick={() => {
              setValue('')
              onSave('')
            }}
            className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-700 disabled:opacity-40"
          >
            Убрать шаблон
          </button>
        )}
      </div>
      {invalid && <p className="mt-1 text-xs text-red-600">Это не id шаблона: нужен UUID вида 0f4a6b2c-1d3e-…</p>}
      <p className="mt-2 text-xs text-gray-500">
        С шаблоном тема и текст берутся из конструктора Unisender. Если шаблон не отправится (удалён, ошибка в id), письмо
        уйдёт нашим текстом, а причина появится в журнале.
      </p>
      <p className="mt-2 text-xs text-amber-700">
        Шаблонизатор — simple. Не вставляйте подстановку внутрь условия ({'{{intro?…{{intro}}…}}'}): Unisender в этом случае
        молча не отправляет письмо, и откат на текст его не спасёт. Условия в ссылках тоже не работают.
      </p>

      <div className="mt-3 text-xs font-semibold uppercase tracking-wider text-gray-500">Подстановки</div>
      <dl className="mt-1.5 grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
        {[...kind.vars, ...commonVars].map((v) => (
          <div key={v.name} className="contents">
            <dt className="font-mono text-gray-900">{`{{${v.name}}}`}</dt>
            <dd className="mb-1 text-gray-500 sm:mb-0">{v.desc}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

const STATUS: Record<LogItem['status'], { label: string; cls: string }> = {
  sent: { label: 'отправлено', cls: 'bg-green-50 text-green-700' },
  failed: { label: 'ошибка', cls: 'bg-red-50 text-red-700' },
  disabled: { label: 'выключено', cls: 'bg-gray-100 text-gray-500' },
}

function fmtDate(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}

export function AdminEmailsRoute() {
  const [kinds, setKinds] = useState<EmailKind[] | null>(null)
  const [commonVars, setCommonVars] = useState<EmailVar[]>([])
  const [templatesActive, setTemplatesActive] = useState(true)
  const [openKey, setOpenKey] = useState<string | null>(null)
  const [log, setLog] = useState<LogPage | null>(null)
  const [query, setQuery] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  function applyEmails(data: EmailsResponse) {
    setKinds(data.kinds)
    setCommonVars(data.commonVars ?? [])
    setTemplatesActive(data.templatesActive ?? true)
  }

  useEffect(() => {
    fetch('/api/admin/emails', { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then(applyEmails)
      .catch((e) => setError(e instanceof Error ? e.message : 'Ошибка загрузки'))
  }, [])

  useEffect(() => {
    const params = new URLSearchParams({ page: String(page) })
    if (search) params.set('q', search)
    fetch(`/api/admin/emails/log?${params}`, { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then(setLog)
      .catch((e) => setError(e instanceof Error ? e.message : 'Ошибка загрузки журнала'))
  }, [page, search])

  const save = (patch: { key: string; enabled?: boolean; templateId?: string }) => {
    setSaving(true)
    setError(null)
    fetch('/api/admin/emails', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then(applyEmails)
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось сохранить'))
      .finally(() => setSaving(false))
  }

  const kindByKey = new Map((kinds ?? []).map((k) => [k.key, k]))
  const groups = [...new Set((kinds ?? []).map((k) => k.group))]
  const pages = log ? Math.max(1, Math.ceil(log.total / log.perPage)) : 1

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="mx-auto max-w-5xl px-4 py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Письма</h1>
            <p className="text-sm text-gray-500">
              Что сервер шлёт клиентам. Тумблер и шаблон действуют сразу, без деплоя. Цепочки Unisender настраиваются в
              самом Unisender.
            </p>
          </div>
          <AdminNav current="/admin/emails" />
        </div>

        {error && <div className="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}

        {kinds !== null && !templatesActive && (
          <div className="mb-4 rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800">
            Шаблоны Unisender сейчас не работают: письма уходят не через Unisender Go (SMTP_UNISENDER_GO не включён). Заданные
            шаблоны сохранятся и заработают после переключения.
          </div>
        )}

        {kinds === null && !error && (
          <div className="flex items-center gap-2 rounded-xl border border-gray-200 bg-white px-5 py-6 text-sm text-gray-500 shadow-sm">
            <RefreshCw className="h-4 w-4 animate-spin" />
            Загружаю…
          </div>
        )}

        {groups.map((group) => (
          <section key={group} className="mb-8">
            <h2 className="mb-3 text-lg font-bold text-gray-900">{group}</h2>
            <div className="rounded-xl border border-gray-200 bg-white shadow-sm">
              {kinds!
                .filter((k) => k.group === group)
                .map((k) => (
                  <div key={k.key} className="border-b border-gray-100 px-5 py-4 last:border-b-0">
                    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5 font-medium text-gray-900">
                          {k.title}
                          <Tooltip kind={k} />
                        </div>
                        <div className="mt-0.5 truncate text-sm text-gray-500">
                          {k.templateId ? 'Тема и текст — из шаблона Unisender' : `Тема: «${k.subject}»`}
                        </div>
                        <button
                          type="button"
                          onClick={() => setOpenKey(openKey === k.key ? null : k.key)}
                          aria-expanded={openKey === k.key}
                          className="mt-1 inline-flex items-center gap-1 text-sm text-rose-600 hover:underline"
                        >
                          {k.templateId ? 'Шаблон Unisender подключён' : 'Шаблон Unisender'}
                          <ChevronDown className={`h-3.5 w-3.5 transition-transform ${openKey === k.key ? 'rotate-180' : ''}`} />
                        </button>
                      </div>
                      <div className="shrink-0">
                        {k.locked ? (
                          <span className="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500">
                            <Lock className="h-3.5 w-3.5" />
                            Всегда отправляется
                          </span>
                        ) : (
                          <Toggle checked={k.enabled} onChange={(v) => save({ key: k.key, enabled: v })} disabled={saving} />
                        )}
                      </div>
                    </div>
                    {openKey === k.key && (
                      <TemplateEditor
                        key={k.templateId}
                        kind={k}
                        commonVars={commonVars}
                        saving={saving}
                        onSave={(templateId) => save({ key: k.key, templateId })}
                      />
                    )}
                  </div>
                ))}
            </div>
          </section>
        ))}

        <section>
          <div className="mb-3 flex flex-wrap items-end justify-between gap-3">
            <div>
              <h2 className="text-lg font-bold text-gray-900">Журнал отправок</h2>
              <p className="text-sm text-gray-500">
                Каждая попытка, включая выключенные и упавшие. {log && `Всего: ${log.total}`}
              </p>
            </div>
            <form
              onSubmit={(e) => {
                e.preventDefault()
                setPage(1)
                setSearch(query.trim())
              }}
              className="flex gap-2"
            >
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="email, id заказа или подписки"
                className="w-64 rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-rose-400 focus:outline-none"
              />
              <button type="submit" className="rounded-lg bg-gray-900 px-3 py-2 text-sm font-medium text-white">
                Найти
              </button>
            </form>
          </div>

          <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-gray-100 text-xs uppercase tracking-wider text-gray-400">
                <tr>
                  <th className="px-4 py-3 font-semibold">Когда</th>
                  <th className="px-4 py-3 font-semibold">Кому</th>
                  <th className="px-4 py-3 font-semibold">Письмо</th>
                  <th className="px-4 py-3 font-semibold">Статус</th>
                  <th className="px-4 py-3 font-semibold">Заказ / подписка</th>
                </tr>
              </thead>
              <tbody>
                {log?.items.map((item) => {
                  const kind = kindByKey.get(item.kind)
                  return (
                    <tr key={item.id} className="border-b border-gray-50 last:border-b-0 align-top">
                      <td className="whitespace-nowrap px-4 py-3 text-gray-500">{fmtDate(item.createdAt)}</td>
                      <td className="px-4 py-3 text-gray-900">{item.recipient}</td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-1.5 text-gray-900">
                          {item.kindTitle}
                          {kind && <Tooltip kind={kind} />}
                        </div>
                        <div className="text-xs text-gray-400">{item.subject}</div>
                      </td>
                      <td className="px-4 py-3">
                        <span className={`rounded-md px-2 py-0.5 text-xs font-medium ${STATUS[item.status]?.cls ?? ''}`}>
                          {STATUS[item.status]?.label ?? item.status}
                        </span>
                        {item.error && <div className="mt-1 max-w-xs break-words text-xs text-red-600">{item.error}</div>}
                        {item.delivery && (
                          <div className="mt-1 text-xs text-gray-500">
                            {item.delivery === 'spam' || item.delivery === 'hard_bounced'
                              ? DELIVERY[item.delivery]
                              : item.openedHumanAt
                                ? 'открыто'
                                : item.openedAt
                                  ? 'открыто почтовиком'
                                  : DELIVERY[item.delivery]}
                          </div>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs">
                        {item.orderRef && (
                          <a href={`/order/${item.orderRef}`} target="_blank" rel="noreferrer" className="text-rose-600 hover:underline">
                            {item.orderId}
                          </a>
                        )}
                        {item.subscriptionId && <div className="text-gray-500">{item.subscriptionId}</div>}
                      </td>
                    </tr>
                  )
                })}
                {log && log.items.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-4 py-6 text-center text-gray-400">
                      {search ? 'Ничего не найдено' : 'Писем ещё не было — журнал ведётся с этого релиза'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {pages > 1 && (
            <div className="mt-3 flex items-center justify-end gap-2 text-sm">
              <button
                type="button"
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
                className="rounded-lg border border-gray-300 bg-white px-3 py-1.5 disabled:opacity-40"
              >
                ←
              </button>
              <span className="text-gray-500">
                {page} / {pages}
              </span>
              <button
                type="button"
                disabled={page >= pages}
                onClick={() => setPage(page + 1)}
                className="rounded-lg border border-gray-300 bg-white px-3 py-1.5 disabled:opacity-40"
              >
                →
              </button>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
