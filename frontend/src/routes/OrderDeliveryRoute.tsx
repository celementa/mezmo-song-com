import { useEffect, useState, useRef } from 'react'
import { useParams } from 'react-router-dom'
import { reportEngagement } from '@/lib/engagement'
import {
  Music, Download, FileText, Loader2, Clock,
  RefreshCw, CheckCircle, ChevronDown, ChevronUp, Sparkles,
} from 'lucide-react'
import type { Loose } from '@/lib/loose'
import { runOrderPurchaseTracking } from '@/features/analytics/purchaseTracking'
import { DeliveryHeldScreen, PostPaymentUpsell } from '@/flow/PostPaymentUpsell'
import { PartnerBrief } from '@/routes/create/PartnerBrief'
import { upsellAlreadySeen } from '@/flow/upsellSeen'
import { fmtCurrency } from '@/routes/config/utils'
import { COMPANY } from '@/lib/site'

type DeliveryConfig = {
  pageTitle?: string
  headerText?: string
  phonePromptText?: string
  processingMessage?: string
  downloadButtonText?: string
  lyricsButtonText?: string
  successMessage?: string
  accentColor?: string
  logoUrl?: string
  trackSubtitleText?: string
  unlockButtonText?: string
  // Revision section
  revisionSectionTitle?: string
  revisionSectionDescription?: string
  revisionTabDescribe?: string
  revisionTabLyrics?: string
  revisionTabNewStory?: string
  revisionSubmitButtonText?: string
  revisionUsedTitle?: string
  revisionUsedMessage?: string
  // Voice labels for revision form
  voiceKeepLabel?: string
  voiceFemaleLabel?: string
  voiceMaleLabel?: string
  // Revision confirmation modal
  revisionConfirmEnabled?: boolean
  revisionConfirmTitle?: string
  revisionConfirmMessage?: string
  revisionConfirmButtonText?: string
  revisionCancelButtonText?: string
  // Testimonial section
  testimonialSectionTitle?: string
  testimonialSectionDescription?: string
  testimonialUploadButtonText?: string
  testimonialSuccessTitle?: string
  testimonialSuccessMessage?: string
  testimonialApprovedMessage?: string
}

type OrderProcessingPageConfig = {
  headline?: string
  subtitle?: string
  countdownLabel?: string
  bottomText?: string
  upsellItemIds?: string[]
  imageUrl?: string
}

type ProcessingUpsellItem = {
  id: string
  icon?: string
  title?: string
  headline?: string
  description?: string
  price?: number
  priceLabel?: string
  ctaText?: string
  action?: string
  actionConfig?: { deliveryTimeMinutes?: number }
}

type OrderStatus = {
  id: string
  trackingId?: string // сырой id заказа — для transaction_id покупки (T-005)
  status: string
  paymentStatus?: string
  xenditInvoiceUrl?: string | null
  recipientName: string
  createdAt: string
  config: DeliveryConfig
  confirmedAt?: string | null
  deliveryScheduledAt?: string | null
  deliveryDelayHours?: number
  revisionWaitHours?: number
  orderProcessingPage?: OrderProcessingPageConfig
  processingUpsellItems?: ProcessingUpsellItem[]
  revisionUpsell?: { enabled: boolean; icon?: string; title?: string; description?: string; price?: number; priceLabel?: string; ctaText?: string } | null
  purchasedUpsells?: Loose[]
  purchaseTrackedAt?: string | null
  isTest?: boolean
  paymentAmount?: number
  pixelConfirmScript?: string | null
  tiktokPixelConfirmScript?: string | null
  hasEmail?: boolean
  upsellMode?: string | null
  acceptedUpsells?: Loose[] | null
  upsellInvoiceUrl?: string | null
  themeSlug?: string | null
  // Поля A/B-флоу (вариант 1): придержанная доставка и апсейлы после оплаты.
  deliveryHeld?: boolean
  flowVariant?: string | null
  /** «Продолжить оформление» неоплаченного заказа воронки /config: анкета восстановится по ref. */
  resumeUrl?: string | null
  upsellKind?: string | null
  subscribed?: boolean
  speedupPurchased?: boolean
  /** Заказ оплачен у партнёра (Les Roses): апсейлов и отбивок покупки нет. */
  partner?: string | null
  /** Партнёрский заказ без анкеты — страница показывает вопросы о получателе. */
  needsBrief?: boolean
}

type UnlockedOrder = {
  id: string
  status: string
  recipientName: string
  createdAt: string
  tracks: string[]
  lyricsText: string | null
  /** Прошлые версии текста, свежие первыми: доработка перезатирает текущий. */
  lyricsVersions?: { version: number; lyricsText: string; createdAt: string }[]
  regenerationCount: number
  maxRegenerations: number
  hasTestimonial: boolean
  testimonialStatus: string | null
  revisionSubmittedAt: string | null
  language: string | null
  musicStyle: string | null
  voiceStyle: string | null
}

function useCountdown(targetTime: number | null) {
  const [remaining, setRemaining] = useState(() => {
    if (!targetTime) return 0
    return Math.max(0, targetTime - Date.now())
  })

  useEffect(() => {
    if (!targetTime) return
    const tick = () => setRemaining(Math.max(0, targetTime - Date.now()))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [targetTime])

  const hours = Math.floor(remaining / (1000 * 60 * 60))
  const minutes = Math.floor((remaining % (1000 * 60 * 60)) / (1000 * 60))
  const seconds = Math.floor((remaining % (1000 * 60)) / 1000)

  return { remaining, hours, minutes, seconds }
}

function RevisionWaitScreen({ order, accent, cfg, revisionWaitHours, refresh }: { order: UnlockedOrder; accent: string; cfg: DeliveryConfig; revisionWaitHours: number; refresh: () => void }) {
  const submittedAt = order.revisionSubmittedAt ? new Date(order.revisionSubmittedAt).getTime() : null
  const waitMs = revisionWaitHours * 60 * 60 * 1000
  const targetTime = submittedAt ? submittedAt + waitMs : null
  const { remaining, hours, minutes, seconds } = useCountdown(targetTime)
  const hasRefreshed = useRef(false)

  useEffect(() => {
    if (targetTime && remaining === 0 && !hasRefreshed.current) {
      hasRefreshed.current = true
      refresh()
    }
  }, [targetTime, remaining, refresh])

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4">
      <div className="text-center max-w-md">
        <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
          <Clock className="h-8 w-8" style={{ color: accent }} />
        </div>
        <h1 className="text-xl font-semibold text-gray-800">
          {cfg.processingMessage || 'Ваша доработка в работе'}
        </h1>
        <p className="mt-2 text-sm text-gray-500">
          Готовим новую версию песни. Примерное время до готовности:
        </p>
        <div className="mt-6 flex justify-center gap-3">
          {[
            { value: hours, label: 'Ч' },
            { value: minutes, label: 'Мин' },
            { value: seconds, label: 'Сек' },
          ].map((unit) => (
            <div key={unit.label} className="flex flex-col items-center">
              <div
                className="flex h-16 w-16 items-center justify-center rounded-xl text-2xl font-bold text-white"
                style={{ backgroundColor: accent }}
              >
                {String(unit.value).padStart(2, '0')}
              </div>
              <span className="mt-1 text-xs text-gray-500">{unit.label}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function OrderProcessingScreen({
  orderStatus,
  accent,
  refresh,
}: {
  orderStatus: OrderStatus
  accent: string
  refresh: () => void
}) {
  const [purchasingId, setPurchasingId] = useState<string | null>(null)
  const [purchaseError, setPurchaseError] = useState<string | null>(null)

  const opp = orderStatus.orderProcessingPage ?? {}
  const upsells = orderStatus.processingUpsellItems ?? []
  const purchasedIds = (orderStatus.purchasedUpsells ?? []).map((u: Loose) => u?.id).filter(Boolean)

  // Calculate countdown target
  let targetTime: number | null = null
  if (orderStatus.deliveryScheduledAt) {
    targetTime = new Date(orderStatus.deliveryScheduledAt).getTime()
  } else if (orderStatus.confirmedAt && orderStatus.deliveryDelayHours && !orderStatus.speedupPurchased) {
    // Купленное ускорение снимает задержку — общий таймер «от оплаты + N часов»
    // ему показывать нельзя, иначе платное ускорение ничего не ускоряет.
    targetTime = new Date(orderStatus.confirmedAt).getTime() + orderStatus.deliveryDelayHours * 60 * 60 * 1000
  }

  const { remaining, hours, minutes, seconds } = useCountdown(targetTime)
  const countdownExpired = targetTime !== null && remaining === 0
  const isFailed = orderStatus.status === 'failed'

  // When countdown expires and order is still processing, refetch every 15s
  useEffect(() => {
    if (!countdownExpired || isFailed) return
    const poll = setInterval(() => {
      refresh()
    }, 15000)
    return () => clearInterval(poll)
  }, [countdownExpired, isFailed, refresh])

  const handleUpsellPurchase = async (upsellId: string) => {
    setPurchasingId(upsellId)
    setPurchaseError(null)
    try {
      const res = await fetch(`/api/public/order/${encodeURIComponent(orderStatus.id)}/upsell-purchase`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ upsellItemId: upsellId }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setPurchaseError(data.error === 'already_purchased' ? 'Дополнение уже куплено.' : 'Произошла ошибка. Попробуйте ещё раз.')
        return
      }
      const data = await res.json()
      if (data.invoiceUrl) {
        window.location.assign(data.invoiceUrl)
      }
    } catch {
      setPurchaseError('Ошибка сети. Попробуйте ещё раз.')
    } finally {
      setPurchasingId(null)
    }
  }

  // Show "still processing" message when countdown expired or order failed
  if (countdownExpired || isFailed) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4 py-12">
        <div className="text-center max-w-md w-full">
          {opp.imageUrl ? (
            <img src={opp.imageUrl} alt="" className="mx-auto mb-4 max-h-24 object-contain" />
          ) : (
            <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
              <Clock className="h-8 w-8" style={{ color: accent }} />
            </div>
          )}
          <h1 className="text-xl font-semibold text-gray-800">
            Заказ ещё готовится
          </h1>
          <p className="mt-2 text-sm text-gray-500">
            Нам нужно чуть больше времени, чем рассчитывали. Мы уже делаем песню и пришлём результат как можно скорее.
          </p>
          <div className="mt-6 flex justify-center">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-200" style={{ borderTopColor: accent }} />
          </div>
          <p className="mt-4 text-xs text-gray-400">
            {orderStatus.hasEmail
              ? 'Когда песня будет готова, отправим уведомление на электронную почту.'
              : 'Страница обновится автоматически, когда песня будет готова.'}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4 py-12">
      <div className="text-center max-w-md w-full">
        {opp.imageUrl ? (
          <img src={opp.imageUrl} alt="" className="mx-auto mb-4 max-h-24 object-contain" />
        ) : (
          <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
            <Clock className="h-8 w-8" style={{ color: accent }} />
          </div>
        )}
        <h1 className="text-xl font-semibold text-gray-800">
          {opp.headline || 'Ваша песня создаётся'}
        </h1>
        <p className="mt-2 text-sm text-gray-500">
          {opp.subtitle || 'Мы делаем персональную песню специально для вас'}
        </p>

        {targetTime && (
          <>
            <p className="mt-5 text-xs font-medium text-gray-400 uppercase tracking-wider">
              {opp.countdownLabel || 'Примерное время до готовности'}
            </p>
            <div className="mt-3 flex justify-center gap-3">
              {[
                { value: hours, label: 'Ч' },
                { value: minutes, label: 'Мин' },
                { value: seconds, label: 'Сек' },
              ].map((unit) => (
                <div key={unit.label} className="flex flex-col items-center">
                  <div
                    className="flex h-16 w-16 items-center justify-center rounded-xl text-2xl font-bold text-white"
                    style={{ backgroundColor: accent }}
                  >
                    {String(unit.value).padStart(2, '0')}
                  </div>
                  <span className="mt-1 text-xs text-gray-500">{unit.label}</span>
                </div>
              ))}
            </div>
          </>
        )}

        <p className="mt-6 text-sm text-gray-500 max-w-xs mx-auto">
          {opp.bottomText
            ? opp.bottomText
            : orderStatus.hasEmail
              ? 'Когда песня будет готова, отправим уведомление на электронную почту.'
              : 'Страница обновится автоматически, когда песня будет готова.'}
        </p>

        {/* Upsell Cards */}
        {upsells.length > 0 && (
          <div className="mt-8 space-y-4">
            {purchaseError && (
              <p className="text-xs text-red-600 bg-red-50 rounded-lg px-3 py-2">{purchaseError}</p>
            )}
            {upsells.map((upsell) => {
              const isPurchased = purchasedIds.includes(upsell.id)
              if (isPurchased) {
                return (
                  <div key={upsell.id} className="rounded-2xl border border-green-200 bg-green-50 p-4 text-center">
                    <CheckCircle className="h-5 w-5 text-green-600 mx-auto mb-1" />
                    <p className="text-sm font-medium text-green-800">{upsell.title} — активно</p>
                  </div>
                )
              }
              return (
                <div key={upsell.id} className="rounded-2xl border bg-white p-5 shadow-sm text-left">
                  <div className="flex items-start gap-3">
                    {upsell.icon && <span className="text-2xl">{upsell.icon}</span>}
                    <div className="flex-1">
                      <h3 className="text-sm font-bold text-gray-900">{upsell.title}</h3>
                      {upsell.description && (
                        <p className="mt-1 text-xs text-gray-500">{upsell.description}</p>
                      )}
                      {upsell.action === 'express_delivery' && upsell.actionConfig?.deliveryTimeMinutes && (
                        <p className="mt-1 text-xs font-medium" style={{ color: accent }}>
                          Получите песню через{' '}
                          {upsell.actionConfig.deliveryTimeMinutes < 60
                            ? `${upsell.actionConfig.deliveryTimeMinutes} мин`
                            : `${Math.round(upsell.actionConfig.deliveryTimeMinutes / 60)} ч`}
                          !
                        </p>
                      )}
                    </div>
                  </div>
                  {(upsell.price ?? 0) > 0 && (
                    <div className="mt-3 rounded-lg bg-gray-50 px-4 py-2.5 text-center">
                      <span className="text-lg font-bold" style={{ color: accent }}>
                        {fmtCurrency(upsell.price ?? 0)}
                      </span>
                      {upsell.priceLabel && <p className="text-xs text-gray-500">{upsell.priceLabel}</p>}
                    </div>
                  )}
                  <button
                    onClick={() => handleUpsellPurchase(upsell.id)}
                    disabled={purchasingId !== null}
                    className="mt-3 w-full rounded-lg px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition-all hover:opacity-90 disabled:opacity-50 flex items-center justify-center gap-2"
                    style={{ backgroundColor: accent }}
                  >
                    {purchasingId === upsell.id ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <>
                        <Sparkles className="h-4 w-4" />
                        {upsell.ctaText || 'Купить'}
                      </>
                    )}
                  </button>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

type RevisionType = 'describe' | 'lyrics' | 'new_story'

function RevisionSection({
  order,
  accent,
  cfg,
  onRegenerated,
  revisionUpsell,
}: {
  order: UnlockedOrder
  accent: string
  cfg: DeliveryConfig
  onRegenerated: () => void
  revisionUpsell?: { enabled: boolean; icon?: string; title?: string; description?: string; price?: number; priceLabel?: string; ctaText?: string } | null
}) {
  const [revisionType, setRevisionType] = useState<RevisionType>('describe')
  const [description, setDescription] = useState('')
  const [newLyrics, setNewLyrics] = useState(order.lyricsText ?? '')
  const [musicStyle, setMusicStyle] = useState('keep')
  const [voiceStyle, setVoiceStyle] = useState('keep')
  const [language, setLanguage] = useState('keep')
  const [submitting, setSubmitting] = useState(false)
  const [purchasing, setPurchasing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showConfirmModal, setShowConfirmModal] = useState(false)

  // maxRegenerations < 0 — безлимит (тестовые заказы из /admin/ads).
  const unlimitedRevisions = order.maxRegenerations < 0
  const revisionsLeft = order.maxRegenerations - order.regenerationCount
  const canRevise = (unlimitedRevisions || revisionsLeft > 0) && order.status === 'completed'

  const handleBuyRevision = async () => {
    setPurchasing(true)
    setError(null)
    try {
      const res = await fetch(`/api/public/order/${encodeURIComponent(order.id)}/revision-upsell-purchase`, {
        method: 'POST',
      })
      if (!res.ok) {
        setError('Произошла ошибка. Попробуйте ещё раз.')
        return
      }
      const data = await res.json()
      if (data.invoiceUrl) {
        window.location.href = data.invoiceUrl
      }
    } catch {
      setError('Ошибка сети. Попробуйте ещё раз.')
    } finally {
      setPurchasing(false)
    }
  }

  if (!canRevise) {
    // Show revision upsell card if enabled
    if (!revisionUpsell?.enabled || order.status !== 'completed') return null

    return (
      <div className="rounded-2xl border bg-white p-5 shadow-sm">
        <div className="flex items-center gap-2 mb-3">
          <RefreshCw className="h-5 w-5" style={{ color: accent }} />
          <h3 className="text-sm font-semibold text-gray-900">{cfg.revisionUsedTitle || 'Доработки закончились'}</h3>
        </div>
        <p className="text-xs text-gray-500 mb-4">
          {cfg.revisionUsedMessage || 'Вы использовали все бесплатные доработки.'}
        </p>
        <div className="rounded-xl border p-4 space-y-3">
          <div className="flex items-center gap-2">
            <span className="text-lg">{revisionUpsell.icon || '🔄'}</span>
            <span className="text-sm font-semibold text-gray-900">{revisionUpsell.title || 'Доработки'}</span>
          </div>
          <p className="text-xs text-gray-500">{revisionUpsell.description || 'Нужны ещё правки? Купите пакет доработок.'}</p>
          <div className="flex items-center justify-between">
            <span className="text-sm font-bold" style={{ color: accent }}>
              {typeof revisionUpsell.price === 'number' ? fmtCurrency(revisionUpsell.price) : ''}
            </span>
            {revisionUpsell.priceLabel && <span className="text-[10px] text-gray-400">{revisionUpsell.priceLabel}</span>}
          </div>
          {error && (
            <p className="text-sm text-red-600 bg-red-50 rounded-lg px-3 py-2">{error}</p>
          )}
          <button
            onClick={handleBuyRevision}
            disabled={purchasing}
            className="w-full rounded-lg px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition-all hover:opacity-90 disabled:opacity-50"
            style={{ backgroundColor: accent }}
          >
            {purchasing ? (
              <span className="flex items-center justify-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Обработка...
              </span>
            ) : (
              revisionUpsell.ctaText || 'Купить доработку'
            )}
          </button>
        </div>
      </div>
    )
  }

  const handleSubmit = async () => {
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch(`/api/public/order/${encodeURIComponent(order.id)}/regenerate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          revisionType,
          description: (revisionType === 'describe' || revisionType === 'new_story') ? description : undefined,
          newLyrics: revisionType === 'lyrics' ? newLyrics : undefined,
          musicStyle: musicStyle !== 'keep' ? musicStyle : undefined,
          voiceStyle: voiceStyle !== 'keep' ? voiceStyle : undefined,
          language: language !== 'keep' ? language : undefined,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError(data.error === 'max_regenerations_reached' ? 'Доработок не осталось.' : 'Произошла ошибка. Попробуйте ещё раз.')
        return
      }
      onRegenerated()
    } catch {
      setError('Ошибка сети. Попробуйте ещё раз.')
    } finally {
      setSubmitting(false)
    }
  }

  const tabs: { key: RevisionType; label: string }[] = [
    { key: 'describe', label: cfg.revisionTabDescribe || 'Описать правки' },
    { key: 'lyrics', label: cfg.revisionTabLyrics || 'Править текст' },
    { key: 'new_story', label: cfg.revisionTabNewStory || 'Новая история' },
  ]

  return (
    <div className="rounded-2xl border bg-white p-5 shadow-sm">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <RefreshCw className="h-5 w-5" style={{ color: accent }} />
          <h3 className="text-sm font-semibold text-gray-900">{cfg.revisionSectionTitle || 'Нужны изменения?'}</h3>
        </div>
        <span
          className="rounded-full px-2.5 py-0.5 text-xs font-semibold text-white"
          style={{ backgroundColor: accent }}
        >
          {unlimitedRevisions ? 'Доработки: без ограничений' : `Осталось доработок: ${revisionsLeft}`}
        </span>
      </div>

      <p className="text-xs text-gray-500 mb-4">
        {cfg.revisionSectionDescription ||
          'Напишите, что хотите поменять — сделаем новую версию. Можно изменить стиль музыки или вокала.'}
      </p>

      <div className="flex gap-1 mb-4 border-b">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setRevisionType(t.key)}
            className={`px-3 py-2 text-xs font-medium transition-colors border-b-2 ${
              revisionType === t.key
                ? 'border-current text-gray-900'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
            style={revisionType === t.key ? { borderColor: accent } : undefined}
          >
            {t.label}
          </button>
        ))}
      </div>

      {revisionType === 'describe' && (
        <div className="space-y-2 mb-4">
          <label className="text-xs text-gray-500">Опишите правки — остальное сделаем мы.</label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={`Например:\n- Заменить имя Иван на Ваня\n- Сделать припев позитивнее\n- Упомянуть нашу поездку в Санкт-Петербург`}
            className="w-full rounded-lg border bg-gray-50 p-3 text-sm min-h-[100px] resize-y focus:outline-none focus:ring-2 focus:border-transparent"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          />
        </div>
      )}

      {revisionType === 'lyrics' && (
        <div className="space-y-2 mb-4">
          <label className="text-xs text-gray-500">Отредактируйте текст ниже — песню запишем ровно по нему.</label>
          <textarea
            value={newLyrics}
            onChange={(e) => setNewLyrics(e.target.value)}
            className="w-full rounded-lg border bg-gray-50 p-3 text-sm min-h-[200px] resize-y font-mono focus:outline-none focus:ring-2 focus:border-transparent"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          />
          {/* Прошлые версии: доработка заменяет текст, и без этого списка удачный
              вариант терялся бы навсегда. Кнопка подставляет его в поле выше —
              отдельной ручки «откатить» не нужно, это обычная доработка. */}
          {(order.lyricsVersions?.length ?? 0) > 0 && (
            <div className="rounded-lg border bg-gray-50 p-3 space-y-2">
              <p className="text-xs font-medium text-gray-700">Прошлые версии текста</p>
              {order.lyricsVersions?.map((v) => (
                <div key={v.version} className="flex items-start gap-2">
                  <button
                    onClick={() => setNewLyrics(v.lyricsText)}
                    className="shrink-0 rounded-full border border-gray-300 bg-white px-3 py-1 text-xs font-medium text-gray-700 hover:bg-gray-100 transition-colors"
                  >
                    Вернуть
                  </button>
                  <span className="text-xs text-gray-500 line-clamp-2">
                    {v.lyricsText.replace(/\[[^\]]*\]/g, ' ').trim().slice(0, 120)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {revisionType === 'new_story' && (
        <div className="space-y-2 mb-4">
          <label className="text-xs text-gray-500">Сделаем новый текст и музыку с нуля.</label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Расскажите новую историю или тему для песни..."
            className="w-full rounded-lg border bg-gray-50 p-3 text-sm min-h-[100px] resize-y focus:outline-none focus:ring-2 focus:border-transparent"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          />
        </div>
      )}

      <div className="grid grid-cols-3 gap-3 mb-4">
        <div>
          <label className="text-xs font-medium text-gray-600 mb-1 block">СТИЛЬ МУЗЫКИ</label>
          <select
            value={musicStyle}
            onChange={(e) => setMusicStyle(e.target.value)}
            className="w-full rounded-lg border bg-gray-50 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:border-transparent appearance-none"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          >
            <option value="keep">{order.musicStyle || 'Как сейчас'}</option>
            <option value="Pop">Pop</option>
            <option value="R&B">R&B</option>
            <option value="Acoustic">Acoustic</option>
            <option value="Jazz">Jazz</option>
            <option value="Rock">Rock</option>
            <option value="Classical">Classical</option>
            <option value="Hip Hop">Hip Hop</option>
            <option value="Country">Country</option>
            <option value="Reggae">Reggae</option>
          </select>
        </div>
        <div>
          <label className="text-xs font-medium text-gray-600 mb-1 block">ВОКАЛ</label>
          <select
            value={voiceStyle}
            onChange={(e) => setVoiceStyle(e.target.value)}
            className="w-full rounded-lg border bg-gray-50 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:border-transparent appearance-none"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          >
            <option value="keep">
              {order.voiceStyle
                ? order.voiceStyle === 'female'
                  ? cfg.voiceFemaleLabel || 'Женский'
                  : cfg.voiceMaleLabel || 'Мужской'
                : cfg.voiceKeepLabel || 'Как сейчас'}
            </option>
            <option value="female">{cfg.voiceFemaleLabel || 'Женский'}</option>
            <option value="male">{cfg.voiceMaleLabel || 'Мужской'}</option>
          </select>
        </div>
        <div>
          <label className="text-xs font-medium text-gray-600 mb-1 block">ЯЗЫК</label>
          <select
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
            className="w-full rounded-lg border bg-gray-50 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:border-transparent appearance-none disabled:opacity-50"
            style={{ '--tw-ring-color': accent + '60' } as React.CSSProperties}
          >
            <option value="keep">Как сейчас</option>
            <option value="Russian">Русский</option>
            <option value="English">Английский</option>
          </select>
        </div>
      </div>

      {/* Галочки «сохранить стиль музыки» здесь больше нет: бэкенд её никогда не
          читал — мелодия менялась на каждой доработке независимо от выбора.
          Вернуть её можно только вместе с Persona у Kie.ai (см. TODO.md), которая
          действительно держит один голос между прогонами. */}

      {error && (
        <p className="text-sm text-red-600 bg-red-50 rounded-lg px-3 py-2 mb-3">{error}</p>
      )}

      <button
        onClick={() => {
          if (cfg.revisionConfirmEnabled === false) {
            handleSubmit()
          } else {
            setShowConfirmModal(true)
          }
        }}
        disabled={submitting || ((revisionType === 'describe' || revisionType === 'new_story') && !description.trim()) || (revisionType === 'lyrics' && !newLyrics.trim())}
        className="w-full rounded-lg px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition-all hover:opacity-90 disabled:opacity-50"
        style={{ backgroundColor: accent }}
      >
        {submitting ? (
          <span className="flex items-center justify-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            Отправка...
          </span>
        ) : (
          cfg.revisionSubmitButtonText || 'Отправить правки'
        )}
      </button>

      {showConfirmModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4" onClick={() => setShowConfirmModal(false)}>
          <div className="bg-white rounded-2xl shadow-xl p-5 w-full max-w-sm space-y-4" onClick={(e) => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-gray-900">
              {cfg.revisionConfirmTitle || 'Перед новой генерацией'}
            </h3>
            <p className="text-sm text-gray-600 whitespace-pre-line">
              {cfg.revisionConfirmMessage ||
                'Скачайте текущие песни до запроса доработки — при перегенерации старые файлы могут быть удалены.\n\nМузыка и звучание могут частично измениться.'}
            </p>
            <div className="flex gap-3">
              <button
                onClick={() => setShowConfirmModal(false)}
                className="flex-1 rounded-lg border border-gray-300 px-4 py-2.5 text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors"
              >
                {cfg.revisionCancelButtonText || 'Отмена'}
              </button>
              <button
                onClick={() => {
                  setShowConfirmModal(false)
                  handleSubmit()
                }}
                className="flex-1 rounded-lg px-4 py-2.5 text-sm font-semibold text-white shadow-sm hover:opacity-90 transition-all"
                style={{ backgroundColor: accent }}
              >
                {cfg.revisionConfirmButtonText || 'Подтвердить'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export function OrderDeliveryRoute() {
  const { orderId } = useParams<{ orderId: string }>()
  const [orderStatus, setOrderStatus] = useState<OrderStatus | null>(null)
  const [unlockedOrders, setUnlockedOrders] = useState<UnlockedOrder[] | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [loading, setLoading] = useState(true)
  const [isRevisionPending, setIsRevisionPending] = useState(false)
  const hasAutoConfirmed = useRef(false)
  const hasFiredPurchaseTracking = useRef(false)
  const [lyricsExpanded, setLyricsExpanded] = useState<Record<string, boolean>>({})
  // Апсейлы после оплаты (шаги 11–12 варианта 1) показываются один раз.
  const [upsellDone, setUpsellDone] = useState(false)

  useEffect(() => {
    if (!orderId) return
    setLoading(true)
    Promise.all([
      fetch(`/api/public/order/${encodeURIComponent(orderId)}/status`),
      fetch(`/api/public/order/${encodeURIComponent(orderId)}/content`),
    ])
      .then(async ([statusRes, contentRes]) => {
        if (!statusRes.ok || !contentRes.ok) {
          setNotFound(true)
          return
        }
        const [statusData, contentData] = await Promise.all([statusRes.json(), contentRes.json()])
        setOrderStatus(statusData)
        setUnlockedOrders(contentData.orders ?? [])
      })
      .catch(() => setNotFound(true))
      .finally(() => setLoading(false))
  }, [orderId])

  // Auto-confirm order if status is 'created' (not yet confirmed) and payment is done
  useEffect(() => {
    if (!orderId || !orderStatus) return
    if (orderStatus.status !== 'created') return
    // Never auto-confirm orders that haven't been paid yet
    if (orderStatus.paymentStatus === 'pending') return
    // Партнёрский заказ ждёт анкету: генерацию запустит её отправка (/config).
    if (orderStatus.needsBrief) return
    if (hasAutoConfirmed.current) return
    hasAutoConfirmed.current = true

    fetch(`/api/orders/${encodeURIComponent(orderId)}/confirm`, { method: 'POST' })
      .then(async () => {
        const statusRes = await fetch(`/api/public/order/${encodeURIComponent(orderId)}/status`)
        if (statusRes.ok) setOrderStatus(await statusRes.json())
      })
      .catch(() => {})
  }, [orderId, orderStatus?.status])

  // Вторая точка браузерной отбивки покупки. Первая — /thanks (return_url), но с
  // T-012 (виджет ЮKassa на своём домене) покупатель туда не попадает: СБП уводит
  // его в банковское приложение, редирект на return_url не срабатывает. По логам
  // с 06.08.2026 браузерных отбивок не было ни одной при 14 оплатах, тогда как
  // страницу заказа (ссылка в письме) открывают штатно. Серверные конверсии
  // (GA4/VK) сюда не относятся — они уходят с вебхука ЮKassa.
  //
  // Задвоить покупку эти две точки не могут: событие уходит только тому, кому
  // сервер ответил first=true на атомарную заявку mark-purchase-tracked.
  useEffect(() => {
    if (!orderStatus) return
    if (hasFiredPurchaseTracking.current) return
    if (orderStatus.status === 'created') return
    if (orderStatus.paymentStatus === 'pending') return
    if (orderStatus.purchaseTrackedAt) return
    // Деньги партнёрского заказа прошли не через нас и не с нашей рекламы.
    if (orderStatus.partner) return
    hasFiredPurchaseTracking.current = true
    void runOrderPurchaseTracking(orderStatus)
  }, [orderStatus])

  // Poll for status updates while order is processing or failed (admin may retry), or revision is pending
  useEffect(() => {
    if (!orderId || !orderStatus) return
    const isProcessing = orderStatus.status === 'processing' || orderStatus.status === 'created' || orderStatus.status === 'failed'
    if (!isProcessing && !isRevisionPending) return

    const poll = async () => {
      try {
        const [statusRes, contentRes] = await Promise.all([
          fetch(`/api/public/order/${encodeURIComponent(orderId)}/status`),
          fetch(`/api/public/order/${encodeURIComponent(orderId)}/content`),
        ])
        if (statusRes.ok) {
          const statusData = await statusRes.json()
          setOrderStatus(statusData)
        }
        if (contentRes.ok) {
          const contentData = await contentRes.json()
          const orders = contentData.orders ?? []
          setUnlockedOrders(orders)
          if (orders.length > 0) setIsRevisionPending(false)
        }
      } catch {
        // сеть моргнула — следующий тик поллинга подхватит статус
      }
    }

    const id = setInterval(poll, 10000)
    return () => clearInterval(id)
  }, [orderId, orderStatus?.status, isRevisionPending])

  const cfg = orderStatus?.config ?? {}
  const accent = cfg.accentColor || '#E11D48'

  const refreshOrders = async () => {
    if (!orderId) return
    try {
      const [statusRes, contentRes] = await Promise.all([
        fetch(`/api/public/order/${encodeURIComponent(orderId)}/status`),
        fetch(`/api/public/order/${encodeURIComponent(orderId)}/content`),
      ])
      if (statusRes.ok) setOrderStatus(await statusRes.json())
      if (contentRes.ok) {
        const data = await contentRes.json()
        const orders = data.orders ?? []
        setUnlockedOrders(orders)
        if (orders.length > 0) setIsRevisionPending(false)
      }
    } catch {
      // сеть моргнула — поллинг обновит заказ сам
    }
  }

  const handleRegenerated = async () => {
    setIsRevisionPending(true)
    await refreshOrders()
  }

  const downloadLyrics = (order: UnlockedOrder) => {
    if (!order.lyricsText) return
    const blob = new Blob([order.lyricsText], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `lyrics-${order.recipientName || order.id}.txt`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const renderLogo = (size: 'lg' | 'sm' = 'lg') => {
    const dimension = size === 'lg' ? 'h-16 w-16' : 'h-10 w-10'
    const iconSize = size === 'lg' ? 'h-8 w-8' : 'h-5 w-5'

    return (
      <div className={`mx-auto mb-4 flex ${dimension} items-center justify-center rounded-full`} style={{ backgroundColor: accent + '15' }}>
        <Music className={iconSize} style={{ color: accent }} />
      </div>
    )
  }

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white">
        <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
      </div>
    )
  }

  if (notFound) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4">
        <div className="text-center max-w-sm">
          <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100">
            <Music className="h-8 w-8 text-gray-400" />
          </div>
          <h1 className="text-xl font-semibold text-gray-800">Заказ не найден</h1>
          <p className="mt-2 text-sm text-gray-500">Такого заказа нет или он удалён.</p>
        </div>
      </div>
    )
  }

  // Песня куплена у партнёра (Les Roses), вопросов о получателе ещё не было —
  // их задаёт эта страница: ссылка из письма ведёт сюда.
  if (orderStatus?.needsBrief && orderId) {
    return <PartnerBrief orderRef={orderId} onDone={() => void refreshOrders()} />
  }

  if (orderStatus?.partner && orderStatus.paymentStatus === 'refunded') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4">
        <div className="text-center max-w-sm">
          {renderLogo('lg')}
          <h1 className="text-xl font-semibold text-gray-800">Заказ отменён</h1>
          <p className="mt-2 text-sm text-gray-500">
            Заказ в магазине отменён, поэтому песня по нему не создаётся. Если это ошибка, напишите нам на {COMPANY.email}.
          </p>
        </div>
      </div>
    )
  }

  if (orderStatus?.paymentStatus === 'pending') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4 py-12">
        <div className="text-center max-w-sm">
          {renderLogo('lg')}
          <h1 className="text-xl font-semibold text-gray-800">Ожидается оплата</h1>
          <p className="mt-3 text-sm text-gray-500">
            Заказ ещё не оплачен. Завершите оплату, чтобы мы начали создание песни.
          </p>
          {orderStatus.xenditInvoiceUrl && (
            <a
              href={orderStatus.xenditInvoiceUrl}
              className="mt-6 inline-block rounded-full px-8 py-3 text-sm font-semibold text-white"
              style={{ backgroundColor: accent }}
            >
              Оплатить
            </a>
          )}
          {!orderStatus.xenditInvoiceUrl && orderStatus.resumeUrl && (
            <a
              href={orderStatus.resumeUrl}
              className="mt-6 inline-block rounded-full px-8 py-3 text-sm font-semibold text-white"
              style={{ backgroundColor: accent }}
            >
              Продолжить оформление
            </a>
          )}
        </div>
      </div>
    )
  }

  // Шаги 11–12: апсейл подписки и стемов сразу после оплаты. Только для заказов
  // нового флоу (flowVariant задан) и не для самих заказов-апсейлов.
  const showPostPaymentUpsell =
    Boolean(orderId) &&
    orderStatus?.paymentStatus === 'paid' &&
    Boolean(orderStatus?.flowVariant) &&
    !orderStatus?.partner &&
    !orderStatus?.upsellKind &&
    !orderStatus?.subscribed &&
    !upsellDone &&
    !upsellAlreadySeen(orderId ?? '')

  if (showPostPaymentUpsell && orderId) {
    return (
      <PostPaymentUpsell
        orderRef={orderId}
        paidAmount={orderStatus?.paymentAmount ?? 0}
        onDone={() => setUpsellDone(true)}
      />
    )
  }

  // Доставка придержана: песня готова, но обычный срок ещё не вышел. Показывать
  // её здесь означало бы, что платное ускорение ничего не ускоряет.
  if (orderStatus?.deliveryHeld) {
    return <DeliveryHeldScreen deliverAt={orderStatus.deliveryScheduledAt ?? null} accent={accent} />
  }

  if (isRevisionPending || (unlockedOrders && unlockedOrders.length === 0)) {
    if (isRevisionPending) {
      return (
        <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4 py-12">
          <div className="text-center max-w-md w-full">
            <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
              <Clock className="h-8 w-8" style={{ color: accent }} />
            </div>
            <h1 className="text-xl font-semibold text-gray-800">Правки в работе</h1>
            <p className="mt-2 text-sm text-gray-500">
              Готовим новую версию вашей песни. Это займёт несколько минут — страница обновится автоматически.
            </p>
            <div className="mt-6 flex justify-center">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-gray-200" style={{ borderTopColor: accent }} />
            </div>
          </div>
        </div>
      )
    }
    // If order is still processing or failed, show processing screen instead of "not found"
    if (orderStatus && (orderStatus.status === 'processing' || orderStatus.status === 'created' || orderStatus.status === 'failed')) {
      return <OrderProcessingScreen orderStatus={orderStatus} accent={accent} refresh={refreshOrders} />
    }
    return (
      <div className="flex min-h-screen items-center justify-center bg-gradient-to-b from-gray-50 to-white px-4">
        <div className="text-center max-w-sm">
          {renderLogo('lg')}
          <h1 className="text-xl font-semibold text-gray-800">Нет данных</h1>
          <p className="mt-2 text-sm text-gray-500">Не удалось найти контент для этого заказа.</p>
        </div>
      </div>
    )
  }

  if (unlockedOrders && unlockedOrders.length > 0) {
    // Check if any order is within revision wait period
    const revisionWaitMs = (orderStatus?.revisionWaitHours ?? 24) * 60 * 60 * 1000
    const revisionWaitOrder = revisionWaitMs > 0 ? unlockedOrders.find((o) => {
      if (!o.revisionSubmittedAt || o.regenerationCount === 0) return false
      const elapsed = Date.now() - new Date(o.revisionSubmittedAt).getTime()
      return elapsed < revisionWaitMs
    }) : undefined

    if (revisionWaitOrder) {
      return <RevisionWaitScreen order={revisionWaitOrder} accent={accent} cfg={cfg} revisionWaitHours={orderStatus?.revisionWaitHours ?? 24} refresh={refreshOrders} />
    }

    const allProcessing = unlockedOrders.every((o) => o.status !== 'completed')

    // Check if delivery is still pending (delivery window not yet reached)
    const deliveryNotReady = orderStatus && (() => {
      if (orderStatus.deliveryScheduledAt) {
        return new Date(orderStatus.deliveryScheduledAt).getTime() > Date.now()
      }
      if (orderStatus.confirmedAt && orderStatus.deliveryDelayHours) {
        const target = new Date(orderStatus.confirmedAt).getTime() + orderStatus.deliveryDelayHours * 60 * 60 * 1000
        return target > Date.now()
      }
      return false
    })()

    if ((allProcessing || deliveryNotReady) && orderStatus) {
      return <OrderProcessingScreen orderStatus={orderStatus} accent={accent} refresh={refreshOrders} />
    }

    return (
      <div className="flex min-h-screen justify-center bg-gradient-to-b from-gray-50 to-white px-4 py-12">
        <div className="w-full max-w-lg">
          <div className="text-center mb-8">
            {renderLogo('lg')}
            <h1 className="text-2xl font-bold text-gray-900">
              {cfg.successMessage || 'Ваша песня готова!'}
            </h1>
          </div>

          <div className="space-y-6">
            {unlockedOrders.map((order) => {
              const isProcessing = order.status !== 'completed'

              if (isProcessing) {
                return (
                  <div key={order.id} className="rounded-2xl border bg-white p-5 shadow-sm">
                    <div className="flex items-center gap-3 mb-2">
                      <div className="flex h-10 w-10 items-center justify-center rounded-full" style={{ backgroundColor: accent + '15' }}>
                        <Loader2 className="h-5 w-5 animate-spin" style={{ color: accent }} />
                      </div>
                      <div>
                        <h3 className="text-sm font-semibold text-gray-900">
                          {order.recipientName ? `Песня для ${order.recipientName}` : 'Ваша песня'}
                        </h3>
                        <p className="text-xs text-gray-500">{cfg.processingMessage || 'Создаём…'}</p>
                      </div>
                    </div>
                  </div>
                )
              }

              const isLyricsOpen = lyricsExpanded[order.id] ?? false

              return (
                <div key={order.id} className="space-y-4">
                  {unlockedOrders.length > 1 && order.recipientName && (
                    <div className="flex items-center gap-2">
                      <h2 className="text-sm font-semibold text-gray-700">
                        {(cfg.headerText || 'Персональная песня для {recipientName}').replace('{recipientName}', order.recipientName)}
                      </h2>
                      <span className="text-xs text-gray-400">
                        {new Date(order.createdAt).toLocaleDateString()}
                      </span>
                    </div>
                  )}

                  <div className="rounded-2xl border bg-white p-5 shadow-sm">
                    <div className="flex items-center gap-2 mb-4">
                      <Music className="h-5 w-5" style={{ color: accent }} />
                      <h3 className="text-sm font-semibold text-gray-900">Прослушать</h3>
                    </div>

                    <div className={order.tracks.length > 1 ? 'grid grid-cols-1 gap-4' : ''}>
                      {order.tracks.map((url, idx) => (
                        <div key={`${order.id}-${idx}`} className="space-y-2">
                          {order.tracks.length > 1 && (
                            <span className="text-xs font-semibold text-gray-500 uppercase tracking-wider">
                              Вариант {idx + 1}
                            </span>
                          )}
                          <audio controls className="w-full h-10" preload="metadata" onPlay={() => reportEngagement(orderId, 'listen')}>
                            <source src={url} />
                          </audio>
                          <a
                            href={url}
                            download
                            onClick={() => reportEngagement(orderId, 'download')}
                            className="flex items-center justify-center gap-2 w-full rounded-lg px-4 py-2.5 text-sm font-semibold text-white transition-all hover:opacity-90"
                            style={{ backgroundColor: accent }}
                          >
                            <Download className="h-4 w-4" />
                            {cfg.downloadButtonText || 'Скачать MP3'}
                          </a>
                        </div>
                      ))}
                    </div>
                  </div>

                  {order.lyricsText && (
                    <div className="rounded-2xl border bg-white shadow-sm overflow-hidden">
                      <button
                        onClick={() => setLyricsExpanded((prev) => ({ ...prev, [order.id]: !isLyricsOpen }))}
                        className="w-full flex items-center justify-between p-5 text-left hover:bg-gray-50 transition-colors"
                      >
                        <div className="flex items-center gap-2">
                          <FileText className="h-5 w-5 text-amber-600" />
                          <h3 className="text-sm font-semibold text-gray-900">Текст песни</h3>
                        </div>
                        {isLyricsOpen ? (
                          <ChevronUp className="h-4 w-4 text-gray-400" />
                        ) : (
                          <ChevronDown className="h-4 w-4 text-gray-400" />
                        )}
                      </button>
                      {isLyricsOpen && (
                        <div className="px-5 pb-5">
                          <div className="rounded-lg bg-gray-50 p-4 text-sm text-gray-700 whitespace-pre-wrap leading-relaxed max-h-80 overflow-y-auto">
                            {order.lyricsText}
                          </div>
                          <button
                            onClick={() => downloadLyrics(order)}
                            className="mt-3 flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 transition-colors"
                          >
                            <Download className="h-3.5 w-3.5" />
                            {cfg.lyricsButtonText || 'Скачать .txt'}
                          </button>
                        </div>
                      )}
                    </div>
                  )}

                  <RevisionSection
                    order={order}
                    accent={accent}
                    cfg={cfg}
                    onRegenerated={handleRegenerated}
                    revisionUpsell={orderStatus?.revisionUpsell}
                  />

                </div>
              )
            })}
          </div>
        </div>
      </div>
    )
  }

  return null
}
