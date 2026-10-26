import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'

import { apiGet, apiPatch, apiPost } from '@/lib/http'
import { getAttribution } from '@/lib/attribution'
import { reportFlowStep } from '@/lib/flowSteps'
import { trackPixelEvent } from '@/features/analytics/MetaPixel'
import { trackTikTokEvent } from '@/features/analytics/TikTokPixel'
import { CREAM } from '@/routes/create/StepShell'
import type { FlowConfig } from './useFlowConfig'
import { getLang, localePath, useLang, useTr } from '@/lib/lang'
import { CUSTOM_GENRE, EMPTY_ANSWERS, GENRES, answerFromWire, answerText, type FlowAnswers } from './types'
import { clearFlowDraft, loadFlowDraft, saveFlowDraft, searchWithStep, stepFromSearch } from './draft'
import { EmailStep, EmotionStep, OccasionStep, RecipientStep, StoryStep, answersGenre } from './steps/BriefSteps'
import { GeneratingStep, PreviewStep, SpeedupStep } from './steps/GenerationSteps'
import { CheckoutStep, CompareStep } from './steps/PaymentSteps'
import type { PaymentMode, PaymentStart } from '@/features/payment/UnitPayCheckout'

/**
 * Флоу варианта 1 (см. CLAUDE.md, «A/B-тесты флоу»).
 *
 * Отдельный компонент, а не режим ConfigRoute: у плеч разный порядок экранов и
 * разный момент создания заказа, а легаси-форма остаётся живым фолбэком на
 * случай, если /api/public/flow недоступен. Смешивать их в одном файле значило
 * бы рисковать работающими продажами ради нового теста.
 *
 * Оплата — виджет ЮKassa на шаге payment (редирект — фолбэк), шаги после
 * оплаты (апсейл подписки, стемы, готово) — на /order/{ref}: этот компонент
 * владеет всем до оплаты включительно.
 */

/** Шаги, которые рисует этот компонент. Остальные из config.steps — чужие. */
const OWNED_STEPS = ['recipient', 'occasion', 'email', 'emotion', 'story', 'generating', 'preview', 'speedup', 'compare', 'payment']

// Стрелку к подписи дорисовывает StepShell.
const NEXT_LABEL: Record<string, [string, string]> = {
  recipient: ['Далее', 'Next'],
  occasion: ['Далее', 'Next'],
  email: ['Продолжить', 'Continue'],
  emotion: ['Далее', 'Next'],
  story: ['Создать песню', 'Create my song'],
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/** Как часто опрашиваем готовность песни во время генерации до оплаты. */
const STATUS_POLL_MS = 3000

type OrderContent = {
  orders: unknown[]
  previewUrl: string | null
}

export function FlowRoute({ config, sessionId }: { config: FlowConfig; sessionId: string }) {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const tr = useTr()
  const lang = useLang()

  // Черновик (см. draft.ts) восстанавливается один раз при монтировании. Шаг
  // берётся из адреса (`?s=N` — «назад»/«вперёд» браузера), иначе из черновика.
  // Черновик уже оплаченного заказа годится только для хода по истории: новый
  // заход на /config без `?s=` — это новая песня, а не возврат к оплаченной.
  const [fromHistory] = useState(() => stepFromSearch(search) !== null)
  // `?order=<ref>` — ссылка «продолжить оформление» из письма или со страницы
  // заказа: анкета восстанавливается по ref с сервера, черновик браузера в
  // расчёт не идёт (на чужом устройстве его нет, а свой может быть от другого
  // заказа).
  const [resumeRef] = useState(() => new URLSearchParams(search).get('order'))
  const [draft] = useState(() => {
    const d = loadFlowDraft(config.flowVariant)
    if (resumeRef && d?.orderRef !== resumeRef) return null
    return d?.paid && !fromHistory ? null : d
  })
  // Оплаченный черновик, не восстановленный выше, больше не нужен.
  useEffect(() => {
    if (!draft) clearFlowDraft()
  }, [draft])
  const [answers, setAnswers] = useState<FlowAnswers>(draft?.answers ?? EMPTY_ANSWERS)
  const [stepIndex, setStepIndex] = useState(() => stepFromSearch(search) ?? draft?.stepIndex ?? 0)
  const [orderRef, setOrderRef] = useState<string | null>(draft?.orderRef ?? null)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [generationError, setGenerationError] = useState<string | null>(null)
  /**
   * Что оплачивается на шаге payment: сама песня или, после экрана сравнения
   * ценового плеча, первый платёж подписки вместо разовой покупки.
   */
  const [payVia, setPayVia] = useState<'order' | 'subscription'>(draft?.payVia ?? 'order')

  // Список своих шагов считается ПОСЛЕ хуков состояния: вызов filter до них
  // сбивает React Compiler, и он бросает мемоизацию всего компонента.
  const ownSteps = config.steps.filter((step) => OWNED_STEPS.includes(step))
  const lastStep = ownSteps.length - 1
  const paymentIdx = ownSteps.indexOf('payment')
  const current = ownSteps[Math.min(stepIndex, lastStep)] ?? 'recipient'

  /** Смена шага: состояние + запись в историю браузера, чтобы «назад» вёл на предыдущий шаг. */
  const stepRef = useRef(stepIndex)
  useEffect(() => {
    stepRef.current = stepIndex
  }, [stepIndex])
  const setStep = useCallback(
    (next: number, replace = false) => {
      const idx = Math.min(Math.max(next, 0), lastStep)
      setStepIndex(idx)
      window.scrollTo(0, 0)
      navigate({ pathname, search: searchWithStep(window.location.search, idx) }, { replace })
    },
    [navigate, pathname, lastStep],
  )

  // Адрес без номера шага (первый заход, восстановление черновика) — дописываем
  // его заменой, чтобы у текущего шага была своя запись в истории.
  useEffect(() => {
    if (stepFromSearch(window.location.search) === null) {
      navigate({ pathname, search: searchWithStep(window.location.search, stepRef.current) }, { replace: true })
    }
  }, [navigate, pathname])

  // «Назад»/«вперёд» браузера меняют `?s=` — идём на этот шаг. Свои переходы
  // через setStep сюда тоже попадают, но там номер уже совпадает.
  const urlStep = stepFromSearch(search)
  useEffect(() => {
    if (urlStep === null || urlStep === stepRef.current) return
    setStepIndex(Math.min(urlStep, lastStep))
    window.scrollTo(0, 0)
  }, [urlStep, lastStep])

  // Восстановились с уже заведённым заказом (вернулись из банка или «назад» со
  // страницы заказа): пока не узнаем, оплачен ли он, шаги не рисуем — на шаге
  // оплаты initiate ответил бы already_paid и снова увёл на /order, и «назад»
  // ходило бы по кругу. Оплачен и пришли по истории — встаём не дальше шага
  // перед оплатой: ответы видно, «Далее» вернёт на страницу заказа. Оплачен и
  // заход новый — это новая песня: черновик стирается, анкета с начала.
  const [checkingPaid, setCheckingPaid] = useState(Boolean(draft?.orderRef))
  const draftOrderRef = draft?.orderRef ?? null
  useEffect(() => {
    if (!checkingPaid || !draftOrderRef) return
    let alive = true
    apiGet<{ paymentStatus?: string }>(`/api/public/order/${encodeURIComponent(draftOrderRef)}/status`)
      .then((st) => {
        if (!alive || st.paymentStatus !== 'paid') return
        if (!fromHistory) {
          clearFlowDraft()
          setAnswers(EMPTY_ANSWERS)
          setOrderRef(null)
          setPayVia('order')
          setStep(0, true)
          return
        }
        const back = Math.min(stepRef.current, Math.max(paymentIdx - 1, 0))
        saveFlowDraft({ flowVariant: config.flowVariant, answers, orderRef, stepIndex: back, payVia, paid: true })
        setStep(back, true)
      })
      .catch(() => {
        // статус не ответил — ведём себя как раньше: показываем оплату
      })
      .finally(() => {
        if (alive) setCheckingPaid(false)
      })
    return () => {
      alive = false
    }
  }, [checkingPaid, draftOrderRef, fromHistory, setStep, paymentIdx, config.flowVariant, answers, orderRef, payVia])

  // Восстановление по ref: ответы с сервера, шаг — первый незаполненный после
  // email. Заказ другого плеча, оплаченный или уже в генерации восстановить
  // нельзя: оплаченный ведём на страницу заказа, остальное — анкета с начала.
  const [resuming, setResuming] = useState(Boolean(resumeRef) && !draft)
  useEffect(() => {
    if (!resuming || !resumeRef) return
    let alive = true
    apiGet<{ flowVariant: string; answers: Partial<FlowAnswers> }>(`/api/public/order/${encodeURIComponent(resumeRef)}/resume`)
      .then((res) => {
        if (!alive) return
        if (res.flowVariant !== config.flowVariant) return
        const genre = res.answers.genre ?? ''
        const known = GENRES.some((g) => g.id === genre)
        // Заказ мог быть создан на английской версии: её подписи возвращаем к ключам.
        const restored: FlowAnswers = {
          ...EMPTY_ANSWERS,
          ...res.answers,
          relationship: answerFromWire(res.answers.relationship ?? ''),
          occasion: answerFromWire(res.answers.occasion ?? ''),
          emotion: answerFromWire(res.answers.emotion ?? ''),
          genre: genre && !known ? CUSTOM_GENRE : genre,
          customStyle: genre && !known ? genre : '',
        }
        setAnswers(restored)
        setOrderRef(resumeRef)
        const emailIdx = ownSteps.indexOf('email')
        let next = emailIdx + 1
        if (restored.emotion || restored.genre) next = ownSteps.indexOf('story')
        if (restored.story) next = ownSteps.indexOf('story') + 1
        setStep(Math.max(next, 0), true)
      })
      .catch((err: unknown) => {
        // 409 not_resumable — заказ оплачен или в генерации: его место на /order.
        if (alive && err instanceof Error && err.message === 'not_resumable') {
          navigate(localePath(`/order/${encodeURIComponent(resumeRef)}`), { replace: true })
        }
      })
      .finally(() => {
        if (alive) setResuming(false)
      })
    return () => {
      alive = false
    }
    // ownSteps пересчитывается каждый рендер, а восстановление — одноразовое.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resuming, resumeRef, config.flowVariant, setStep, navigate])

  // Черновик пишется при каждом изменении: ответы, заказ, шаг, что оплачиваем.
  useEffect(() => {
    if (checkingPaid || resuming) return
    saveFlowDraft({ flowVariant: config.flowVariant, answers, orderRef, stepIndex, payVia, paid: draft?.paid })
  }, [checkingPaid, resuming, config.flowVariant, answers, orderRef, stepIndex, payVia, draft?.paid])

  // Номер шага — из полного списка, а не из своего: покупатель видит «шаг 3 из
  // 13», где 13 включает оплату и экраны после неё.
  const globalStep = config.steps.indexOf(current) + 1

  // Показ шага уходит на сервер: из этого дашборд строит «где отпадают люди».
  // Флоу помечается как variant1 — это воронка /config, а не движок /create.
  useEffect(() => {
    reportFlowStep('variant1', current, config.steps.indexOf(current), config.flowVariant)
  }, [current, config.steps, config.flowVariant])

  // Существующая цель GTM «дошёл до выбора стиля» (см. ANALYTICS.md §3): в новом
  // флоу этот шаг четвёртый, но событие обязано остаться тем же — иначе триггер
  // и цель Метрики перестанут срабатывать, а воронка разойдётся с легаси-плечом.
  const styleStepTracked = useRef(false)
  useEffect(() => {
    if (current !== 'emotion' || styleStepTracked.current) return
    styleStepTracked.current = true
    const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
    dataLayer.push({ event: 'reach_style_step', flow_variant: config.flowVariant, language: getLang() })
  }, [current, config.flowVariant])

  const patch = useCallback((fields: Partial<FlowAnswers>) => {
    setAnswers((prev) => ({ ...prev, ...fields }))
    setError(null)
  }, [])

  /** Прыжок сразу на шаг оплаты (сбой генерации, отказ от превью и т.п.). */
  const goToCheckout = useCallback(
    (_ref: string) => {
      setStep(paymentIdx >= 0 ? paymentIdx : lastStep)
    },
    [setStep, paymentIdx, lastStep],
  )

  /** Переход к следующему своему шагу. */
  const advance = useCallback(
    (_ref: string | null) => {
      setStep(stepRef.current + 1)
    },
    [setStep],
  )

  /** Платёж за песню; return_url ставит сервер (/order/{ref}). */
  const startPayment = useCallback(
    (mode: PaymentMode) => {
      if (!orderRef) return Promise.reject(new Error('no_order'))
      return apiPost<PaymentStart>(`/api/orders/${encodeURIComponent(orderRef)}/initiate-payment`, { mode })
    },
    [orderRef],
  )

  /** Ценовое плечо: подписка вместо разовой покупки — первый платёж подписки. */
  const startSubscription = useCallback(
    (mode: PaymentMode) => {
      if (!orderRef) return Promise.reject(new Error('no_order'))
      return apiPost<PaymentStart>(`/api/orders/${encodeURIComponent(orderRef)}/subscribe-instead`, { mode })
    },
    [orderRef],
  )

  /** Платить нечего (dev-режим, уже оплачен, подписка активна) — на страницу заказа. */
  const goToOrder = useCallback(() => {
    if (orderRef) navigate(localePath(`/order/${encodeURIComponent(orderRef)}`))
  }, [orderRef, navigate])

  /** Заказ создаётся на шаге email: дальше он и есть черновик. */
  const createOrder = useCallback(async () => {
    const attribution = getAttribution()
    const gaMatch = typeof document !== 'undefined' ? document.cookie.match(/_ga=GA\d\.\d\.(\d+\.\d+)/) : null
    const ymMatch = typeof document !== 'undefined' ? document.cookie.match(/_ym_uid=(\d+)/) : null

    const res = await apiPost<{ orderId: string; flowVariant?: string; basePrice?: number }>('/api/orders/draft', {
      sessionId,
      email: answers.email.trim(),
      whatsappNumber: answers.phone.trim() || null,
      recipientName: answers.recipientName.trim(),
      yourName: answers.recipientName.trim(),
      occasion: answerText(answers.occasion, lang),
      relationship: answerText(answers.relationship, lang),
      ...(answers.recipientGender ? { recipientGender: answers.recipientGender } : {}),
      ...(gaMatch ? { gaClientId: gaMatch[1] } : {}),
      ...(ymMatch ? { ymClientId: ymMatch[1] } : {}),
      ...(attribution ? { attribution } : {}),
    })
    setOrderRef(res.orderId)
    trackPixelEvent('Lead', { content_name: 'AI Song', currency: 'USD', value: 0 }, `order_created:${res.orderId}`)
    trackTikTokEvent('SubmitForm', { content_name: 'AI Song', currency: 'USD', value: 0 })
    const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
    dataLayer.push({ event: 'flow_order_created', flow_variant: config.flowVariant })
    return res.orderId
  }, [answers, sessionId, config.flowVariant, lang])

  const validate = useCallback((): string | null => {
    if (current === 'recipient') {
      if (!answers.relationship) return tr('Выберите, кому дарим песню.', 'Choose who the song is for.')
      if (!answers.recipientName.trim()) return tr('Укажите имя.', 'Enter a name.')
      if (!answers.recipientGender && (answers.relationship === 'Друг' || answers.relationship === 'Другое')) {
        return tr('Уточните, ему или ей.', 'Please specify: him or her.')
      }
    }
    if (current === 'occasion' && !answers.occasion) return tr('Выберите повод.', 'Choose an occasion.')
    if (current === 'email' && !EMAIL_RE.test(answers.email.trim())) return tr('Укажите корректный email.', 'Enter a valid email.')
    if (current === 'emotion' && !answers.emotion && !answersGenre(answers)) {
      return tr('Выберите эмоцию или жанр.', 'Choose an emotion or a genre.')
    }
    return null
  }, [current, answers, tr])

  const handleNext = useCallback(async () => {
    const invalid = validate()
    if (invalid) {
      setError(invalid)
      return
    }
    setBusy(true)
    setError(null)
    try {
      let ref = orderRef
      if (current === 'email' && !ref) {
        ref = await createOrder()
      }
      if (current === 'emotion' && ref) {
        await apiPatch(`/api/orders/${encodeURIComponent(ref)}/brief`, {
          emotion: answerText(answers.emotion, lang),
          musicPreferences: { genre: answersGenre(answers), language: tr('Russian', 'English') },
        })
      }
      if (current === 'story' && ref) {
        await apiPatch(`/api/orders/${encodeURIComponent(ref)}/brief`, { story: answers.story.trim() })
      }
      advance(ref)
    } catch {
      setError(tr('Не получилось сохранить. Попробуйте ещё раз.', "Couldn't save that. Please try again."))
    } finally {
      setBusy(false)
    }
  }, [validate, current, orderRef, createOrder, answers, advance, lang, tr])

  const handleBack = useCallback(() => {
    setError(null)
    setStep(stepRef.current - 1)
  }, [setStep])

  // Шаг 6: запуск генерации до оплаты и опрос статуса. Отказ по крану — не
  // ошибка: шаги генерации и превью пропускаются, покупатель идёт к оплате.
  const generationStarted = useRef(false)
  useEffect(() => {
    if (current !== 'generating' || !orderRef || generationStarted.current) return
    generationStarted.current = true
    let alive = true
    let timer: ReturnType<typeof setTimeout> | undefined

    // Готовность определяется по контенту, а не по order.status: в mock-api
    // готовый заказ остаётся в processing, и опрос по статусу в dev не
    // завершился бы никогда.
    const poll = async () => {
      if (!alive) return
      try {
        const content = await apiGet<OrderContent>(`/api/public/order/${encodeURIComponent(orderRef)}/content`)
        if (!alive) return
        const ready = (content.orders?.length ?? 0) > 0 || Boolean(content.previewUrl)
        if (ready) {
          if (content.previewUrl) {
            setPreviewUrl(content.previewUrl)
            advance(orderRef)
          } else {
            // Песня готова, а отрывок не собрался (нет MinIO, не тот формат) —
            // держать человека на пустом плеере незачем, ведём к оплате.
            goToCheckout(orderRef)
          }
          return
        }
      } catch {
        // Сеть моргнула — продолжаем опрос, страница уже показывает ожидание.
      }
      timer = setTimeout(poll, STATUS_POLL_MS)
    }

    apiPost(`/api/orders/${encodeURIComponent(orderRef)}/generate-preview`, { sessionId })
      .then(() => poll())
      .catch(() => {
        if (alive) goToCheckout(orderRef)
      })

    return () => {
      alive = false
      if (timer) clearTimeout(timer)
    }
  }, [current, orderRef, sessionId, advance, goToCheckout])

  const buySpeedup = useCallback(async () => {
    if (!orderRef) return
    setBusy(true)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(orderRef)}/speedup`, {})
    } catch {
      // Ускорение не купилось — это не повод не дать оплатить песню.
    } finally {
      setBusy(false)
      advance(orderRef)
    }
  }, [orderRef, advance])

  // Оболочку карточки (прогресс, заголовок, навигацию) рисует StepShell внутри
  // каждого шага — дизайн 1:1 с движком /create. Номер шага глобальный:
  // покупатель видит «шаг 3 из 13», где 13 включает оплату и экраны после неё.
  const shell = {
    stepIndex: globalStep - 1,
    stepTotal: config.steps.length,
    onBack: stepIndex === 0 ? () => navigate(localePath('/')) : handleBack,
    canGoBack: true,
    busy,
    error,
  }
  const nextLabel = NEXT_LABEL[current] ?? ['Далее', 'Next']
  const briefProps = { answers, patch, shell, onNext: handleNext, primaryLabel: tr(nextLabel[0], nextLabel[1]) }

  if (checkingPaid || resuming) {
    return (
      <div className="flex min-h-screen items-center justify-center" style={{ background: CREAM }}>
        <Loader2 className="h-8 w-8 animate-spin text-[#B9852E]" />
      </div>
    )
  }

  return (
    <div className="min-h-screen font-sans" style={{ background: CREAM }}>
      {current === 'recipient' && <RecipientStep {...briefProps} />}
      {current === 'occasion' && <OccasionStep {...briefProps} />}
      {current === 'email' && <EmailStep {...briefProps} />}
      {current === 'emotion' && <EmotionStep {...briefProps} />}
      {current === 'story' && <StoryStep {...briefProps} />}
      {current === 'generating' && (
        <GeneratingStep
          shell={shell}
          error={generationError}
          onRetry={() => {
            setGenerationError(null)
            generationStarted.current = false
          }}
        />
      )}
      {current === 'preview' && (
        <PreviewStep
          shell={shell}
          previewUrl={previewUrl}
          recipientName={answers.recipientName}
          occasion={answers.occasion}
          onUnlock={() => advance(orderRef)}
        />
      )}
      {current === 'speedup' && (
        <SpeedupStep
          shell={shell}
          price={config.speedupPrice}
          basePrice={config.price}
          delayHours={config.deliveryDelayHours}
          busy={busy}
          onAccept={buySpeedup}
          onSkip={() => advance(orderRef)}
        />
      )}
      {current === 'compare' && (
        <CompareStep
          shell={shell}
          oneTimePrice={config.price}
          subscriptionPrice={config.subscriptionPrice}
          defaultChoice={config.compareDefault === 'onetime' ? 'onetime' : 'subscription'}
          busy={busy}
          onSubscribe={() => {
            setPayVia('subscription')
            advance(orderRef)
          }}
          onOneTime={() => {
            setPayVia('order')
            advance(orderRef)
          }}
        />
      )}
      {current === 'payment' && (
        <CheckoutStep
          shell={shell}
          amount={payVia === 'subscription' ? config.subscriptionPrice : config.price}
          original={payVia === 'subscription' ? null : config.originalPrice}
          perMonth={payVia === 'subscription'}
          summary={
            <ul className="space-y-1">
              <li>{tr('Кому', 'For')}: {answers.recipientName.trim()}{answers.relationship ? ` (${answerText(answers.relationship, lang).toLowerCase()})` : ''}</li>
              {answers.occasion && <li>{tr('Повод', 'Occasion')}: {answerText(answers.occasion, lang)}</li>}
              <li>{tr('Песня придёт на', 'Song will be sent to')} {answers.email.trim()}</li>
            </ul>
          }
          start={payVia === 'subscription' ? startSubscription : startPayment}
          onPaid={goToOrder}
        />
      )}
    </div>
  )
}
