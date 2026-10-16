/**
 * Воронка /create — оба новых варианта на одном движке.
 *
 * Вариант (v1 «оплата поздно» / v2 «оплата в начале») назначает СЕРВЕР — та же
 * ротация плеч, что на /config (useFlowConfig): жребия на фронте больше нет,
 * иначе он перебивал бы серверное назначение и ломал сравнимость A/B-теста.
 * `?v=1` / `?v=2` — принудительно (ручная проверка), выбор закрепляется в
 * localStorage (variant.ts). Порядок шагов задан данными (flowConfig.ts),
 * поэтому оба варианта — это один и тот же набор экранов в разной
 * последовательности, а не две копии кода.
 *
 * Старый флоу никуда не делся и живёт на /old-flow.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'

import { apiGet, apiPost } from '@/lib/http'
import { getAttribution } from '@/lib/attribution'
import { getSessionId } from '@/lib/session'
import { runOrderPurchaseTracking } from '@/features/analytics/purchaseTracking'
import { cachedSettingsGet } from '@/lib/settingsCache'
import { useFlowConfig } from '@/flow/useFlowConfig'
import type { PaymentMode, PaymentStart } from '@/features/payment/useEmbeddedPayment'

import { createStepTracker } from './analytics'
import { NO_BACK_STEPS, stepsFor, type StepId } from './flowConfig'
import { EMPTY_FORM, type FlowFormState, type FlowOrderState, type StepProps } from './types'
import { resolveVariant, type FlowVariant } from './variant'
import { resolveGenre } from '@/flow/types'
import { EmailStep, GeneratingStep, ListenStep } from './steps/SongDeliverySteps'
import { EmotionStep, OccasionStep, RecipientStep, StoryStep } from './steps/SongSteps'
import {
  DoneStep,
  PaymentStep,
  UpsellSpeedStep,
  UpsellStemsStep,
  UpsellSubscriptionStep,
} from './steps/CommerceSteps'

const DRAFT_KEY = 'mezmo:create_draft:v1'
const SUBSCRIPTION_PRICE = 990

type OrderStatus = {
  paymentStatus?: string | null
  flow?: string | null
  paymentAmount?: number | null
  discountCode?: string | null
  deliveryHeld?: boolean
  deliveryScheduledAt?: string | null
  // Подписка уже оформлена с этого заказа (или песня по подписке) — апсейл
  // подписки не повторяется.
  subscribed?: boolean
  extras?: { prices?: Record<string, number>; selected?: string[]; paid?: string[] }
  // Поля браузерной отбивки покупки: воронка возвращается с оплаты в себя, а не
  // на /thanks, поэтому событие purchase отбивает она сама (ровно один раз —
  // заявку выдаёт сервер, см. runOrderPurchaseTracking).
  id?: string
  trackingId?: string
  status?: string
  purchaseTrackedAt?: string | null
  isTest?: boolean
  pixelConfirmScript?: string | null
  tiktokPixelConfirmScript?: string | null
}

type PersistedDraft = {
  variant: FlowVariant
  stepId: StepId
  form: FlowFormState
  orderId: string
  tracks: string[]
  previewUrl?: string | null
}

/**
 * Черновик воронки: шаг, ответы и id заказа. Нужен, чтобы возврат с оплаты
 * ЮKassa (внешний редирект) продолжал воронку с того же места, а не начинал её
 * заново. Черновик чужого варианта игнорируется — порядок шагов у них разный.
 */
function restoreDraft(variant: FlowVariant, orderFromUrl: string | null) {
  let draft: PersistedDraft | null = null
  try {
    const raw = localStorage.getItem(DRAFT_KEY)
    const parsed = raw ? (JSON.parse(raw) as PersistedDraft) : null
    if (parsed && parsed.variant === variant) draft = parsed
  } catch {
    /* приватный режим — черновика нет */
  }
  return {
    stepId: draft?.stepId ?? null,
    form: { ...EMPTY_FORM, ...(draft?.form ?? {}) },
    orderId: orderFromUrl || draft?.orderId || '',
    tracks: draft?.tracks ?? [],
    previewUrl: draft?.previewUrl ?? null,
  }
}

/** Состояние заказа с сервера; null — сеть моргнула, состояние не меняем. */
async function fetchOrderStatus(id: string): Promise<OrderStatus | null> {
  try {
    return await apiGet<OrderStatus>(`/api/public/order/${encodeURIComponent(id)}/status`)
  } catch {
    return null
  }
}

export function CreateRoute() {
  const variant = useMemo<FlowVariant | null>(() => resolveVariant(window.location.search), [])
  const orderFromUrl = useMemo(() => new URLSearchParams(window.location.search).get('order'), [])
  if (variant) return <CreateFlow variant={variant} />
  if (orderFromUrl) return <VariantFromOrder orderId={orderFromUrl} />
  return <ServerAssignedVariant />
}

/**
 * Ссылка на заказ (?order=…) без закреплённого варианта: возврат с оплаты
 * в другом браузере, письмо-догонка, приватный режим. Вариант хранится у
 * заказа (`flow`), и спрашивать ротацию плеч здесь нельзя — она может выдать
 * другой вариант или вовсе увести в /config, потеряв оплаченный заказ.
 */
function VariantFromOrder({ orderId }: { orderId: string }) {
  const [variant, setVariant] = useState<FlowVariant | 'unknown' | null>(null)
  useEffect(() => {
    let alive = true
    void fetchOrderStatus(orderId).then((res) => {
      if (!alive) return
      const flow = res?.flow
      setVariant(flow === 'v1' || flow === 'v2' ? resolveVariant(`?v=${flow}`) : 'unknown')
    })
    return () => {
      alive = false
    }
  }, [orderId])
  if (variant === 'unknown') return <ServerAssignedVariant />
  if (!variant) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
      </div>
    )
  }
  return <CreateFlow variant={variant} />
}

/**
 * /create без ?v и без закреплённого варианта: плечо спрашивается у сервера.
 * Плечо варианта 2 ведёт на свой /create?v=N (?v закрепится через variant.ts);
 * плечо варианта 1 или недоступная ручка — посетитель принадлежит /config
 * (там FlowEntry отрисует его плечо либо легаси-форму).
 */
function ServerAssignedVariant() {
  const state = useFlowConfig()
  const navigate = useNavigate()

  useEffect(() => {
    if (state.status === 'loading') return
    const route = state.status === 'ready' ? state.config.route ?? '' : ''
    navigate(route.startsWith('/create') ? route : '/config', { replace: true })
  }, [state, navigate])

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
    </div>
  )
}

function CreateFlow({ variant }: { variant: FlowVariant }) {
  const navigate = useNavigate()

  // Бесплатной генерации не досталось (дневная квота или суточный бюджет) —
  // песня будет после оплаты, а шаги «пишем песню» и «слушайте» из воронки
  // выпадают. Заказ при этом создан: раньше сервер отвечал 429 без заказа, и
  // человек не мог даже купить.
  const [generationSkipped, setGenerationSkipped] = useState(false)
  const steps = useMemo(() => {
    let list = stepsFor(variant)
    if (generationSkipped) list = list.filter((id) => id !== 'generating' && id !== 'listen')
    return list
  }, [variant, generationSkipped])
  // Плечо приходит вместе с ценой (/api/public/flow) — с ним показы шагов на
  // дашборде разрезаются по плечам, а не только по варианту воронки.
  const [arm, setArm] = useState<string | null>(null)
  const track = useMemo(() => createStepTracker(variant, steps.length, arm), [variant, steps.length, arm])

  // Черновик читается ОДИН раз при первом рендере, а не эффектом: иначе первый
  // кадр показывал бы первый шаг, а следом — восстановленный (каскадный рендер
  // и мигание на возврате с оплаты).
  const initial = useMemo(() => restoreDraft(variant, new URLSearchParams(window.location.search).get('order')), [variant])

  // Возврат с оплаты (?paid=1 ставит сервер в return_url): воронка сразу
  // встаёт на шаг после оплаты, не дожидаясь статуса заказа. Иначе, пока
  // вебхук ЮKassa не доехал, а черновика в этом браузере нет (оплата через
  // приложение банка открывает ссылку в другом браузере; приватный режим),
  // человек видел первый вопрос анкеты — «переадресация на начало флоу».
  const [stepIndex, setStepIndex] = useState(() => {
    const paidReturn = new URLSearchParams(window.location.search).get('paid') === '1'
    const paymentIndex = steps.indexOf('payment')
    const fromDraft = Math.max(0, steps.indexOf(initial.stepId ?? steps[0]))
    if (paidReturn && paymentIndex >= 0 && fromDraft <= paymentIndex) {
      return Math.min(paymentIndex + 1, steps.length - 1)
    }
    return fromDraft
  })
  const [form, setFormState] = useState<FlowFormState>(initial.form)
  const [order, setOrder] = useState<FlowOrderState>({
    orderId: initial.orderId,
    paymentStatus: null,
    extras: [],
    extraPrices: {},
    paymentAmount: null,
    discountCode: null,
    deliveryHeld: false,
    deliverAt: null,
    subscribed: false,
  })
  const [tracks, setTracks] = useState<string[]>(initial.tracks)
  // Отрывок 15 секунд — то, что можно слушать ДО оплаты: полный трек прокси
  // без оплаты не отдаёт (и не должен).
  const [previewUrl, setPreviewUrl] = useState<string | null>(initial.previewUrl)
  const [price, setPrice] = useState(990)
  const [originalPrice, setOriginalPrice] = useState(2990)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const stepId = steps[stepIndex] ?? steps[0]
  const setForm = useCallback((patch: Partial<FlowFormState>) => setFormState((prev) => ({ ...prev, ...patch })), [])

  // --- цены из публичных настроек ---
  useEffect(() => {
    let alive = true
    // Цена берётся из конфигурации ПЛЕЧА, а не из общих настроек: у плеча она
    // своя (flow.price.<плечо>), и заказ создаётся именно по ней. Пока витрина
    // читала PAYMENT_AMOUNT_RUBLES, смена цены плеча в админке разводила показ
    // и списание — показывало 990, списывало 1250. Если плечо ведёт не в эту
    // воронку (зашли по прямой ссылке ?v=), сервер заводит заказ по env-цене —
    // тогда и на витрине остаётся она.
    const sid = getSessionId()
    void apiGet<{ price?: number; originalPrice?: number; route?: string; flowVariant?: string }>(
      `/api/public/flow?sid=${encodeURIComponent(sid)}`,
    )
      .then((cfg) => {
        if (!alive) return
        const belongsHere = typeof cfg?.route === 'string' && cfg.route.startsWith('/create')
        // Плечо пишем только если ротация действительно привела в эту воронку —
        // прямые заходы по ?v= не должны пачкать разрез теста.
        if (belongsHere && cfg?.flowVariant) setArm(cfg.flowVariant)
        if (belongsHere && typeof cfg?.price === 'number') setPrice(cfg.price)
        if (typeof cfg?.originalPrice === 'number') setOriginalPrice(cfg.originalPrice)
        if (belongsHere) return
        // Плечо не про эту воронку — цена из общих настроек, как и у сервера.
        void cachedSettingsGet<{ paymentAmount?: number }>('/api/public/settings').then((st) => {
          if (alive && typeof st?.paymentAmount === 'number') setPrice(st.paymentAmount)
        })
      })
      .catch(() => {
        void cachedSettingsGet<{ paymentAmount?: number; originalAmount?: number }>('/api/public/settings')
          .then((st) => {
            if (!alive) return
            if (typeof st?.paymentAmount === 'number') setPrice(st.paymentAmount)
            if (typeof st?.originalAmount === 'number') setOriginalPrice(st.originalAmount)
          })
          .catch(() => {})
      })
    return () => {
      alive = false
    }
  }, [])

  // --- сохранение черновика ---
  useEffect(() => {
    try {
      // Дошли до конца — черновик стирается. Иначе возврат на /create (а именно
      // туда ведёт письмо-догонка с промокодом на вторую песню) восстанавливал
      // шаг «Готово» с прошлым заказом, и купить ещё одну было нельзя вообще:
      // «Назад» на этом шаге запрещён.
      if (stepId === 'done') {
        localStorage.removeItem(DRAFT_KEY)
        return
      }
      const draft: PersistedDraft = { variant, stepId, form, orderId: order.orderId, tracks, previewUrl }
      localStorage.setItem(DRAFT_KEY, JSON.stringify(draft))
    } catch {
      /* приватный режим — черновик не переживёт перезагрузку */
    }
  }, [variant, stepId, form, order.orderId, tracks, previewUrl])

  // --- синхронизация состояния заказа с сервером ---
  const applyOrderStatus = useCallback((id: string, res: OrderStatus) => {
    const extras = [...(res.extras?.selected ?? []), ...(res.extras?.paid ?? [])]
    setOrder((prev) => ({
      ...prev,
      orderId: id,
      paymentStatus: res.paymentStatus ?? null,
      extras: Array.from(new Set(extras)),
      extraPrices: res.extras?.prices ?? prev.extraPrices,
      paymentAmount: typeof res.paymentAmount === 'number' ? res.paymentAmount : prev.paymentAmount,
      discountCode: res.discountCode ?? null,
      deliveryHeld: Boolean(res.deliveryHeld),
      deliverAt: res.deliveryScheduledAt ?? null,
      subscribed: Boolean(res.subscribed),
    }))
  }, [])

  // Покупка в браузере отбивается один раз на заказ. Точка отбивки нужна
  // здесь потому, что заказы воронок возвращаются с оплаты в саму воронку
  // (return_url → /create?order=…&paid=1), а не на /thanks: без этого у них
  // не было браузерного purchase вообще, только серверные конверсии.
  const purchaseFiredRef = useRef(false)
  const paidAdvanceRef = useRef(false)
  const maybeTrackPurchase = useCallback((id: string, res: OrderStatus) => {
    if (purchaseFiredRef.current) return
    if (res.paymentStatus !== 'paid' || !res.status || res.status === 'created') return
    purchaseFiredRef.current = true
    void runOrderPurchaseTracking({
      id: res.id ?? id,
      trackingId: res.trackingId,
      status: res.status,
      paymentStatus: res.paymentStatus,
      purchaseTrackedAt: res.purchaseTrackedAt,
      isTest: res.isTest,
      paymentAmount: res.paymentAmount,
      pixelConfirmScript: res.pixelConfirmScript,
      tiktokPixelConfirmScript: res.tiktokPixelConfirmScript,
    })
  }, [])

  const refreshOrder = useCallback(
    async (id: string) => {
      if (!id) return null
      const res = await fetchOrderStatus(id)
      if (res) {
        applyOrderStatus(id, res)
        maybeTrackPurchase(id, res)
      }
      return res
    },
    [applyOrderStatus, maybeTrackPurchase],
  )

  // Состояние заказа подтягивается после его появления и КОРОТКО опрашивается,
  // пока сервер ещё не знает про оплату.
  //
  // Одного запроса не хватало: возврат с оплаты (основной или допника) обычно
  // случается раньше, чем доедет вебхук ЮKassa, и человек видел «Оплатить» или
  // «Добавить за 290 ₽» на то, что уже купил. Опрос идёт только пока есть чего
  // ждать: заказ не оплачен либо на возврате указан допник (?extra=), который
  // ещё не отмечен оплаченным.
  const orderId = order.orderId
  const expectedExtra = useMemo(() => new URLSearchParams(window.location.search).get('extra') ?? '', [])
  useEffect(() => {
    if (!orderId) return
    let alive = true
    let attempts = 0

    const settled = (res: OrderStatus): boolean => {
      if (res.paymentStatus !== 'paid') return false
      if (!expectedExtra) return true
      return (res.extras?.paid ?? []).includes(expectedExtra)
    }

    const sync = async () => {
      if (!alive) return
      const res = await fetchOrderStatus(orderId)
      if (!alive) return
      if (res) {
        applyOrderStatus(orderId, res)
        maybeTrackPurchase(orderId, res)
        if (settled(res)) return
      }
      // Двадцать попыток по 3 секунды — минута ожидания вебхука. Дальше молчим:
      // серверная сторона доедет сама, а бесконечный опрос из вкладки не нужен.
      attempts += 1
      if (attempts < 20) setTimeout(sync, 3000)
    }
    void sync()
    return () => {
      alive = false
    }
  }, [orderId, applyOrderStatus, maybeTrackPurchase, expectedExtra])

  // Треки оплаченного заказа дотягиваются с сервера, если в черновике их нет:
  // после оплаты через приложение банка ссылка возврата открывается в другом
  // браузере, черновик (и треки в нём) остаётся в прежнем — без этого финал
  // показывал «готово» без кнопки скачивания. Опрос короткий: песня может ещё
  // генерироваться, тогда её принесёт следующий тик.
  useEffect(() => {
    if (!orderId || order.paymentStatus !== 'paid' || tracks.length > 0 || order.deliveryHeld) return
    let alive = true
    let attempts = 0
    const load = async () => {
      if (!alive) return
      try {
        const res = await apiGet<{ orders?: Array<{ status?: string; tracks?: string[] }>; previewUrl?: string | null }>(
          `/api/public/order/${encodeURIComponent(orderId)}/content`,
        )
        const ready = res.orders?.find((o) => (o.tracks?.length ?? 0) > 0)
        if (!alive) return
        if (ready) {
          setTracks(ready.tracks ?? [])
          if (res.previewUrl) setPreviewUrl(res.previewUrl)
          return
        }
      } catch {
        /* сеть моргнула — следующая попытка */
      }
      attempts += 1
      if (attempts < 40) setTimeout(load, 4000)
    }
    void load()
    return () => {
      alive = false
    }
  }, [orderId, order.paymentStatus, order.deliveryHeld, tracks.length])

  // --- трекинг показа шага ---
  const viewedRef = useRef<string>('')
  useEffect(() => {
    const key = `${stepId}:${stepIndex}`
    if (viewedRef.current === key) return
    viewedRef.current = key
    track(stepId, stepIndex, 'view')
  }, [stepId, stepIndex, track])

  // --- навигация ---
  const goNext = useCallback(() => {
    setError(null)
    setStepIndex((i) => Math.min(i + 1, steps.length - 1))
  }, [steps.length])

  const goBack = useCallback(() => {
    setError(null)
    track(stepId, stepIndex, 'back')
    setStepIndex((i) => Math.max(i - 1, 0))
  }, [stepId, stepIndex, track])

  const canGoBack = stepIndex > 0 && !NO_BACK_STEPS.includes(stepId)

  // --- серверные действия шагов ---

  const songPayload = useCallback(() => {
    const attribution = getAttribution()
    const gaMatch = document.cookie.match(/_ga=GA\d\.\d\.(\d+\.\d+)/)
    const ymMatch = document.cookie.match(/_ym_uid=(\d+)/)
    return {
      recipientName: form.recipientName.trim(),
      relationship: form.relationship,
      occasion: form.occasion,
      story: form.story.trim(),
      // Эмоция — основной выбор, жанр (или свой стиль) — необязательное
      // уточнение. Выбора голоса больше нет: пол вокала сервер выводит из
      // адресата (vocalGenderFor).
      emotion: form.emotion,
      musicPreferences: {
        genre: resolveGenre(form.genre, form.customStyle),
        language: 'Russian',
      },
      ...(form.email.trim() ? { email: form.email.trim().toLowerCase() } : {}),
      // Вариант воронки едет в атрибуции заказа — это ключ сравнения A/B на бэке.
      attribution: { ...(attribution ?? {}), flowVariant: variant },
      // Сессия — та же, что в /api/public/track: по ней сервер сверяет плечо
      // ротации и помечает заказ flow_variant, если назначение совпало.
      sessionId: getSessionId(),
      ...(gaMatch ? { gaClientId: gaMatch[1] } : {}),
      ...(ymMatch ? { ymClientId: ymMatch[1] } : {}),
    }
  }, [form, variant])

  /** v1: создать заказ и запустить генерацию (после шага «история»). */
  const startGeneration = useCallback(async () => {
    if (order.orderId) {
      goNext()
      return
    }
    setBusy(true)
    setError(null)
    try {
      const res = await apiPost<{
        orderId: string
        paymentStatus?: string
        generationSkipped?: boolean
        reason?: string
      }>('/api/flow/v1/draft', songPayload())
      setOrder((prev) => ({ ...prev, orderId: res.orderId, paymentStatus: res.paymentStatus ?? null }))
      if (res.generationSkipped) {
        // Шаги генерации и прослушивания выпадают; переход к следующему шагу
        // произойдёт уже по отфильтрованному списку.
        setGenerationSkipped(true)
        track(stepId, stepIndex, 'skip', { reason: res.reason ?? 'generation_skipped' })
      }
      goNext()
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      track(stepId, stepIndex, 'error', { code: msg })
      setError('Не получилось начать создание песни. Попробуйте ещё раз.')
    } finally {
      setBusy(false)
    }
  }, [order.orderId, songPayload, goNext, stepId, stepIndex, track])

  /** v2: старт заказа по email. v1: привязка email к готовому заказу. */
  const submitEmail = useCallback(async () => {
    setBusy(true)
    setError(null)
    const email = form.email.trim().toLowerCase()
    const phone = form.phone.trim()
    try {
      if (variant === 'v2' && !order.orderId) {
        const attribution = getAttribution()
        const gaMatch = document.cookie.match(/_ga=GA\d\.\d\.(\d+\.\d+)/)
        const ymMatch = document.cookie.match(/_ym_uid=(\d+)/)
        const res = await apiPost<{ orderId: string; paymentStatus?: string }>('/api/flow/v2/start', {
          email,
          phone,
          sessionId: getSessionId(),
          attribution: { ...(attribution ?? {}), flowVariant: variant },
          ...(gaMatch ? { gaClientId: gaMatch[1] } : {}),
          ...(ymMatch ? { ymClientId: ymMatch[1] } : {}),
        })
        setOrder((prev) => ({ ...prev, orderId: res.orderId, paymentStatus: res.paymentStatus ?? null }))
      } else if (order.orderId) {
        await apiPost(`/api/orders/${encodeURIComponent(order.orderId)}/email`, { email, phone })
      }
      goNext()
    } catch (e) {
      track(stepId, stepIndex, 'error', { code: e instanceof Error ? e.message : '' })
      setError('Не получилось сохранить адрес. Проверьте email и попробуйте ещё раз.')
    } finally {
      setBusy(false)
    }
  }, [variant, order.orderId, form.email, form.phone, goNext, stepId, stepIndex, track])

  /** v2: вопросы о песне доезжают после оплаты — отправляем и идём в генерацию. */
  const submitSongConfig = useCallback(async () => {
    if (!order.orderId) {
      setError('Заказ не найден. Начните заново с первого шага.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(order.orderId)}/config`, songPayload())
      goNext()
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      track(stepId, stepIndex, 'error', { code: msg })
      setError(
        msg === 'already_processing'
          ? 'Песня уже создаётся — подождите немного.'
          : 'Не получилось сохранить ответы. Попробуйте ещё раз.',
      )
      if (msg === 'already_processing') goNext()
    } finally {
      setBusy(false)
    }
  }, [order.orderId, songPayload, goNext, stepId, stepIndex, track])

  /** Допник до оплаты — уйдёт в основной платёж. */
  const selectExtra = useCallback(
    async (id: string) => {
      if (!order.orderId) {
        goNext()
        return
      }
      setBusy(true)
      setError(null)
      try {
        await apiPost(`/api/orders/${encodeURIComponent(order.orderId)}/extras/select`, { id })
        await refreshOrder(order.orderId)
        goNext()
      } catch {
        setError('Не получилось добавить опцию. Попробуйте ещё раз.')
      } finally {
        setBusy(false)
      }
    },
    [order.orderId, refreshOrder, goNext],
  )

  /** Допник после оплаты — отдельный платёж с редиректом обратно в воронку. */
  const payExtra = useCallback(
    async (id: string) => {
      if (!order.orderId) {
        goNext()
        return
      }
      setBusy(true)
      setError(null)
      try {
        const res = await apiPost<{ confirmationUrl?: string; paid?: boolean }>(
          `/api/orders/${encodeURIComponent(order.orderId)}/extras/initiate`,
          { id },
        )
        if (res.confirmationUrl) {
          window.location.assign(res.confirmationUrl)
          return
        }
        await refreshOrder(order.orderId)
        goNext()
      } catch {
        setError('Не получилось оформить доплату. Попробуйте ещё раз.')
      } finally {
        setBusy(false)
      }
    },
    [order.orderId, refreshOrder, goNext],
  )

  /** Основная оплата песни: платёж заводится в режиме, который просит виджет. */
  const startPayment = useCallback(
    async (mode: PaymentMode) => {
      if (!order.orderId) throw new Error('no_order')
      try {
        return await apiPost<PaymentStart>(`/api/orders/${encodeURIComponent(order.orderId)}/initiate-payment`, { mode })
      } catch (e) {
        const msg = e instanceof Error ? e.message : ''
        track(stepId, stepIndex, 'error', { code: msg })
        throw e
      }
    },
    [order.orderId, stepId, stepIndex, track],
  )

  /** Платить нечего: цена обнулилась промокодом, dev-режим или заказ уже оплачен. */
  const paidWithoutWidget = useCallback(async () => {
    if (order.orderId) await refreshOrder(order.orderId)
    goNext()
  }, [order.orderId, refreshOrder, goNext])

  /**
   * Апсейл «безлимит за 0 ₽ сегодня»: подписка включается по карте, сохранённой
   * платежом за песню. Карта не сохранилась — обычное оформление на /subscribe.
   */
  const subscribeFromOrder = useCallback(async () => {
    if (!order.orderId) {
      goNext()
      return
    }
    setBusy(true)
    setError(null)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(order.orderId)}/subscribe`, {})
      await refreshOrder(order.orderId)
      goNext()
    } catch (e) {
      const msg = e instanceof Error ? e.message : ''
      track(stepId, stepIndex, 'error', { code: msg })
      if (msg === 'no_saved_method') {
        navigate(`/subscribe?email=${encodeURIComponent(form.email.trim())}`)
        return
      }
      if (msg === 'already_active') {
        setError('У этого email уже есть подписка — сегодняшняя песня и так ваша.')
        return
      }
      setError('Не получилось оформить подписку. Песня уже ваша — попробуйте позже.')
    } finally {
      setBusy(false)
    }
  }, [order.orderId, refreshOrder, goNext, navigate, form.email, stepId, stepIndex, track])

  // --- общие пропсы шага ---
  const base: StepProps = {
    variant,
    stepId,
    stepIndex,
    stepTotal: steps.length,
    form,
    setForm,
    order,
    price,
    originalPrice,
    onNext: goNext,
    onBack: goBack,
    canGoBack,
    track,
    error,
    busy,
  }

  const shareUrl = order.orderId
    ? `${window.location.origin}/order/${encodeURIComponent(order.orderId)}`
    : window.location.origin
  // Оплачен ли заказ — этим, а не позицией шага, определяется поведение
  // апсейлов и сводки: в v2 оплата случается раньше вопросов о песне.
  const paid = order.paymentStatus === 'paid'

  // Возврат с оплаты: заказ уже оплачен, а воронка стоит на шаге оплаты или
  // раньше. Черновик остановился на «Оплатить» — покупатель, вернувшийся по
  // return_url, видел бы кнопку ещё раз. Черновика нет вовсе (СБП: оплата в
  // банковском приложении, ссылка открыта в другом браузере; приватный режим)
  // — по ?order= восстанавливается только id заказа, и без прыжка оплаченный
  // заказ показывал бы первый вопрос анкеты. Прыгаем на шаг после оплаты: в v1
  // это апсейлы, в v2 — вопросы о песне, порядок задаёт flowConfig.
  const paymentIndex = steps.indexOf('payment')
  useEffect(() => {
    if (!paid || paidAdvanceRef.current || stepIndex > paymentIndex) return
    paidAdvanceRef.current = true
    setError(null)
    setStepIndex(Math.min(paymentIndex + 1, steps.length - 1))
  }, [paid, stepIndex, paymentIndex, steps.length])

  switch (stepId) {
    case 'recipient':
      return <RecipientStep {...base} />
    case 'occasion':
      return <OccasionStep {...base} />
    case 'emotion':
      return <EmotionStep {...base} />
    case 'story':
      return (
        <StoryStep
          {...base}
          onNext={() => {
            void (variant === 'v1' ? startGeneration() : submitSongConfig())
          }}
        />
      )
    case 'generating':
      return (
        <GeneratingStep
          {...base}
          onReady={(readyTracks, _lyrics, readyPreviewUrl) => {
            setTracks(readyTracks)
            setPreviewUrl(readyPreviewUrl)
            goNext()
          }}
        />
      )
    case 'listen':
      // paid решает, что играет плеер: до оплаты — отрывок 15 секунд (полный
      // трек прокси без оплаты не отдаёт), после — сама песня.
      return <ListenStep {...base} tracks={tracks} previewUrl={previewUrl} paid={paid} shareUrl={shareUrl} />
    case 'email':
      return <EmailStep {...base} isFirstStep={stepIndex === 0} onNext={() => void submitEmail()} />
    case 'upsellSpeed':
      return (
        <UpsellSpeedStep
          {...base}
          afterPayment={paid}
          onAccept={() => void (paid ? payExtra('speed') : selectExtra('speed'))}
        />
      )
    case 'payment':
      return <PaymentStep {...base} start={startPayment} onPaid={() => void paidWithoutWidget()} />
    case 'upsellSub':
      return (
        <UpsellSubscriptionStep
          {...base}
          subscriptionPrice={SUBSCRIPTION_PRICE}
          subscribed={order.subscribed}
          onSubscribe={() => void subscribeFromOrder()}
        />
      )
    case 'upsellStems':
      return <UpsellStemsStep {...base} onAccept={() => void payExtra('stems')} />
    case 'done':
      return (
        <DoneStep
          {...base}
          tracks={tracks}
          orderUrl={shareUrl}
          onRestart={() => {
            try {
              localStorage.removeItem(DRAFT_KEY)
            } catch {
              /* приватный режим — черновика и не было */
            }
            // Полная перезагрузка, а не сброс состояния: воронка получает чистый
            // заказ, куки и квоту, как у нового посетителя.
            window.location.assign(`/create?v=${variant === 'v2' ? '2' : '1'}`)
          }}
        />
      )
    default:
      return <RecipientStep {...base} />
  }
}
