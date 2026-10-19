import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { AdminNav } from '@/components/admin/AdminNav'

// Страница настроек, меняемых без пересборки и деплоя: значения лежат в таблице
// settings на сервере. Доступна только роли admin — роль ads сюда не проходит.
//
// У каждой настройки подпись, что она делает, и подсказка, какие значения имеют
// смысл: иначе через месяц непонятно, что означает «0» в поле лимита.

type FlowArm = {
  id: string
  label: string
  /** Готовая ссылка «открыть это плечо» — её копируют в рекламные кампании. */
  link: string
  price: number
  weight: number
  enabled: boolean
}

type Settings = {
  trackTestOrders: boolean
  regenerationsClient: number
  regenerationsInternal: number
  // Краны A/B-флоу (вариант 1). 0 в любом числовом поле означает «выключено /
  // без ограничений» — та же конвенция, что у лимитов доработок.
  flowArms: FlowArm[]
  previewEnabled: boolean
  /** Просить ли ЮKassa сохранить карту при оплате песни (см. SettingRow ниже). */
  saveCardInFunnel: boolean
  previewDailyBudget: number
  freeGenPerVisitor: number
  freeGenPerIP: number
  deliveryDelayMinutes: number
  speedupPrice: number
  stemsPrice: number
}

type SettingsPatch = Partial<Omit<Settings, 'flowArms'>> & {
  flowArms?: { id: string; price?: number; weight?: number; enabled?: boolean }[]
}

/** Обёртка одной настройки: заголовок, объяснение и сам контрол справа. */
function SettingRow({
  title,
  hint,
  children,
}: {
  title: string
  hint: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-3 border-b border-gray-100 px-5 py-5 last:border-b-0 sm:flex-row sm:items-start sm:justify-between">
      <div className="max-w-xl">
        <div className="font-medium text-gray-900">{title}</div>
        <p className="mt-1 text-sm leading-relaxed text-gray-500">{hint}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

/**
 * Числовое поле лимита. Ввод хранится строкой, чтобы не мешать набору, и
 * отправляется по потере фокуса или Enter; мусор откатывается к серверному
 * значению. Синхронизация с сервером — через key на стороне родителя: пришло
 * новое значение → поле пересоздалось с ним, без эффекта на каждый ререндер.
 */
function LimitInput({
  value,
  onCommit,
  disabled,
  max = 99,
  min = 0,
}: {
  value: number
  onCommit: (v: number) => void
  disabled: boolean
  max?: number
  min?: number
}) {
  const [draft, setDraft] = useState(String(value))

  const commit = () => {
    const n = Number(draft)
    if (!Number.isInteger(n) || n < min || n > max) {
      setDraft(String(value))
      return
    }
    if (n !== value) onCommit(n)
  }

  return (
    <input
      type="number"
      min={min}
      max={max}
      value={draft}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
      }}
      className="w-24 rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-rose-400 focus:outline-none disabled:opacity-60"
    />
  )
}

/** Короткое текстовое поле (промокод): та же логика подтверждения, что у LimitInput. */
function Toggle({
  checked,
  onChange,
  disabled,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  disabled: boolean
}) {
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
      {checked ? 'Включено' : 'Выключено'}
    </button>
  )
}

export function AdminSettingsRoute() {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [savedAt, setSavedAt] = useState<number | null>(null)

  useEffect(() => {
    fetch('/api/admin/settings', { headers: { Accept: 'application/json' } })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then(setSettings)
      .catch((e) => setError(e instanceof Error ? e.message : 'Ошибка загрузки'))
  }, [])

  // Патч частичный: на сервер уходит только изменённая настройка, ответ —
  // актуальное состояние всех.
  const save = (patch: SettingsPatch) => {
    setSaving(true)
    setError(null)
    fetch('/api/admin/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then((data) => {
        setSettings(data)
        setSavedAt(Date.now())
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Не удалось сохранить'))
      .finally(() => setSaving(false))
  }

  const busy = saving || settings === null

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="mx-auto max-w-4xl px-4 py-8">
        <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Настройки</h1>
            <p className="text-sm text-gray-500">
              Меняются на живом сервере — пересборка и деплой не нужны
              {savedAt && <span className="ml-1 text-green-600">· сохранено</span>}
            </p>
          </div>
          <AdminNav current="/admin/settings" />
        </div>

        {error && (
          <div className="mb-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        )}

        {settings === null && !error && (
          <div className="flex items-center gap-2 rounded-xl border border-gray-200 bg-white px-5 py-6 text-sm text-gray-500 shadow-sm">
            <RefreshCw className="h-4 w-4 animate-spin" />
            Загружаю настройки…
          </div>
        )}

        {settings && (
          <div className="rounded-xl border border-gray-200 bg-white shadow-sm">
            <SettingRow
              title="Доработок клиенту"
              hint="Сколько раз покупатель может отправить песню на доработку со страницы заказа. Каждая доработка — это ещё один платный прогон генерации, поэтому здесь обычно 1–3. Значение 0 снимает ограничение совсем: клиент сможет пересоздавать песню бесконечно. По умолчанию 2."
            >
              <LimitInput
                key={`client-${settings.regenerationsClient}`}
                value={settings.regenerationsClient}
                onCommit={(v) => save({ regenerationsClient: v })}
                disabled={busy}
              />
            </SettingRow>

            <SettingRow
              title="Доработок для тестовых заказов"
              hint="То же самое, но для заказов, созданных на странице «Создать песню» (/admin/ads) — на них блогеры и модераторы подбирают текст. Ставьте 0, если хотите безлимит (так и настроено сейчас), или конечное число, если нужно прижать расход: например 10. Заказ считается тестовым по метке INTERNAL и нулевой цене."
            >
              <LimitInput
                key={`internal-${settings.regenerationsInternal}`}
                value={settings.regenerationsInternal}
                onCommit={(v) => save({ regenerationsInternal: v })}
                disabled={busy}
              />
            </SettingRow>

            <SettingRow
              title="Тестовые заказы в аналитику"
              hint="Включает браузерные события покупки (Метрика-ecommerce, пиксели) и для тестовых заказов. Нужно только когда проверяете, что аналитика вообще срабатывает: тестовые заказы не проходят через платёжного провайдера, поэтому серверных конверсий у них нет. В обычной работе держите выключенным, иначе тестовые прогоны попадут в отчёты как продажи."
            >
              <Toggle
                checked={settings.trackTestOrders}
                onChange={(v) => save({ trackTestOrders: v })}
                disabled={busy}
              />
            </SettingRow>
          </div>
        )}

        {settings && (
          <>
            <h2 className="mb-3 mt-8 text-lg font-bold text-gray-900">A/B-тест флоу</h2>
            <div className="rounded-xl border border-gray-200 bg-white shadow-sm">
              {settings.flowArms.map((arm) => (
                <SettingRow
                  key={arm.id}
                  title={arm.label}
                  hint={
                    (arm.enabled
                      ? `Плечо в ротации. Вес ${arm.weight} — доля трафика считается как вес этого плеча к сумме весов включённых. Контрольное плечо участвует сразу в двух тестах, поэтому веса обычно неравные (например 40 и 20).`
                      : 'Плечо выключено: этой цены и этого порядка шагов не увидит никто. Включайте, когда готовы делить трафик — при десятках визитов в сутки значимости придётся ждать месяцами.') +
                    (arm.enabled
                      ? ` Ссылка для рекламы: ${arm.link} — закрепляет плечо за браузером на 30 дней.`
                      : ` Ссылка ${arm.link} сейчас НЕ РАБОТАЕТ: выключенное плечо не форсится, и человек по ней уйдёт в обычную ротацию по чужой цене. Заходы по нерабочим ссылкам видны на дашборде — панель «Ссылки с нерабочим плечом».`)
                  }
                >
                  <div className="flex items-center gap-2">
                    <label className="flex flex-col text-[10px] font-semibold uppercase tracking-wider text-gray-400">
                      Цена, $
                      <LimitInput
                        key={`price-${arm.id}-${arm.price}`}
                        value={arm.price}
                        min={1}
                        max={100000}
                        onCommit={(v) => save({ flowArms: [{ id: arm.id, price: v }] })}
                        disabled={busy}
                      />
                    </label>
                    <label className="flex flex-col text-[10px] font-semibold uppercase tracking-wider text-gray-400">
                      Вес
                      <LimitInput
                        key={`weight-${arm.id}-${arm.weight}`}
                        value={arm.weight}
                        min={1}
                        max={1000}
                        onCommit={(v) => save({ flowArms: [{ id: arm.id, weight: v }] })}
                        disabled={busy}
                      />
                    </label>
                    <Toggle
                      checked={arm.enabled}
                      onChange={(v) =>
                        save({
                          // Состав ротации сервер собирает из полного списка:
                          // прислать одно плечо значило бы выключить остальные.
                          flowArms: settings.flowArms.map((item) => ({
                            id: item.id,
                            enabled: item.id === arm.id ? v : item.enabled,
                          })),
                        })
                      }
                      disabled={busy}
                    />
                  </div>
                </SettingRow>
              ))}
            </div>

            <h2 className="mb-3 mt-8 text-lg font-bold text-gray-900">Генерация до оплаты</h2>
            <div className="rounded-xl border border-gray-200 bg-white shadow-sm">
              <SettingRow
                title="Генерация до оплаты (превью)"
                hint="Рубильник на всю затею «сначала послушай, потом заплати». Выключишь — шаги «пишем вашу песню» и «слушайте» пропадают из воронок, и человек идёт сразу к оплате; песню он получит после неё. Нужен, когда Kie.ai отвечает долго и превью превращается в бесконечное ожидание: возможность купить важнее возможности послушать заранее."
              >
                <Toggle
                  checked={settings.previewEnabled}
                  onChange={(v) => save({ previewEnabled: v })}
                  disabled={busy}
                />
              </SettingRow>

              <SettingRow
                title="Сохранять карту при оплате песни"
                hint="Только для ЮKassa — на оплату через UnitPay не влияет. Включено — платёж за песню просит ЮKassa сохранить способ оплаты, и апсейл «Безлимит за 0 ₽ сегодня» после оплаты включается в один клик. С 2026-09-09 ЮKassa разрешила магазину автоплатежи по всем способам (карта, СБП, SberPay, T-Pay), поэтому сохранение форму больше не сужает — держите включённым. Выключено — сохранения нет, апсейл ведёт на отдельное оформление подписки."
              >
                <Toggle
                  checked={settings.saveCardInFunnel}
                  onChange={(v) => save({ saveCardInFunnel: v })}
                  disabled={busy}
                />
              </SettingRow>

              <SettingRow
                title="Бесплатных генераций на человека в сутки"
                hint="Сколько раз один человек может получить песню, ничего не заплатив. Считается по подписанной куке посетителя и работает одинаково во всех воронках. Кука удаляется в один клик, поэтому от скрипта это не защита — для этого ниже потолок на адрес и суточный бюджет; зато это честный лимит для живого покупателя. Когда лимит исчерпан, заказ всё равно создаётся, просто песня будет после оплаты."
              >
                <LimitInput
                  key={`fgv-${settings.freeGenPerVisitor}`}
                  value={settings.freeGenPerVisitor}
                  max={50}
                  onCommit={(v) => save({ freeGenPerVisitor: v })}
                  disabled={busy}
                />
              </SettingRow>
              <SettingRow
                title="Бесплатных генераций на IP-адрес в сутки"
                hint="Страховка от скрипта в цикле, а НЕ лимит на человека: за одним адресом сидит офис, кафе или сотни абонентов мобильного оператора, поэтому потолок здесь на порядок выше. Если приложение не видит реального адреса посетителя (прокси не пробросил X-Forwarded-For), проверка по IP автоматически пропускается — иначе один счётчик схлопнул бы весь сайт; в логе тогда появится предупреждение."
              >
                <LimitInput
                  key={`fgi-${settings.freeGenPerIP}`}
                  value={settings.freeGenPerIP}
                  max={100000}
                  onCommit={(v) => save({ freeGenPerIP: v })}
                  disabled={busy}
                />
              </SettingRow>
              <SettingRow
                title="Суточный бюджет генераций до оплаты"
                hint="Потолок на весь сайт за сутки. Когда он исчерпан, шаги «пишем песню» и «отрывок» из флоу выпадают, а оплата остаётся — покупатель просто платит до генерации. Это защита счёта от удачной рекламной кампании и от ботов. 0 — без потолка."
              >
                <LimitInput
                  key={`pb-${settings.previewDailyBudget}`}
                  value={settings.previewDailyBudget}
                  max={100000}
                  onCommit={(v) => save({ previewDailyBudget: v })}
                  disabled={busy}
                />
              </SettingRow>
            </div>

            <h2 className="mb-3 mt-8 text-lg font-bold text-gray-900">Доставка и апсейлы</h2>
            <div className="rounded-xl border border-gray-200 bg-white shadow-sm">
              <SettingRow
                title="Задержка обычной доставки, минут"
                hint="На сколько придерживается готовая песня у обычного заказа. Ускорение продаёт снятие ИМЕННО этой задержки: при 0 песня уходит сразу, ускорять нечего — и шаг с ускорением тогда не показывается вообще, даже если цена задана. Задержка не касается тестовых заказов и песен подписчиков. По умолчанию 0."
              >
                <LimitInput
                  key={`delay-${settings.deliveryDelayMinutes}`}
                  value={settings.deliveryDelayMinutes}
                  max={10080}
                  onCommit={(v) => save({ deliveryDelayMinutes: v })}
                  disabled={busy}
                />
              </SettingRow>
              <SettingRow
                title="Цена ускорения, $"
                hint="Апсейл перед оплатой: снимает задержку выше. Сумма прибавляется к цене заказа ДО оплаты, поэтому платёж остаётся один. 0 — шаг скрыт."
              >
                <LimitInput
                  key={`speedup-${settings.speedupPrice}`}
                  value={settings.speedupPrice}
                  max={100000}
                  onCommit={(v) => save({ speedupPrice: v })}
                  disabled={busy}
                />
              </SettingRow>
              <SettingRow
                title="Цена стемов, $"
                hint="Апсейл после оплаты: инструментал и вокал отдельными дорожками. Создаётся отдельным заказом. Держите 0, пока стемы нечем отдать: разделение дорожек у Kie.ai не подключено, и оплаченный апсейл придётся выполнять руками."
              >
                <LimitInput
                  key={`stems-${settings.stemsPrice}`}
                  value={settings.stemsPrice}
                  max={100000}
                  onCommit={(v) => save({ stemsPrice: v })}
                  disabled={busy}
                />
              </SettingRow>
            </div>
          </>
        )}

        <p className="mt-4 text-xs text-gray-400">
          Значения применяются сразу для новых запросов. Уже открытая у клиента страница
          заказа подхватит новый лимит доработок при следующем обновлении.
        </p>
      </div>
    </div>
  )
}
