import { useCallback, useEffect, useMemo, useState, useRef } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { Check, Clock, Music, Loader2, ArrowLeft, CreditCard } from 'lucide-react'

import { apiGet, apiPost } from '@/lib/http'
import type { Loose } from '@/lib/loose'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'
import { trackTikTokEvent } from '@/features/analytics/TikTokPixel'
import { YooKassaWidget } from '@/features/payment/YooKassaWidget'

type OrderInputPayload = {
  yourName?: string
  recipientName?: string
  occasion?: string
  story?: string
  musicPreferences?: Record<string, string | undefined>
  extraNotes?: string
}

type OrderSummary = {
  id: string
  status: string
  deliveryStatus: string
  paymentStatus: string
  inputPayload: OrderInputPayload
  customer?: { email?: string | null }
  trackUrl?: string | null
  errorMessage?: string | null
  themeSlug?: string | null
  xenditInvoiceUrl?: string | null
  basePrice?: number | null
  discountCode?: string | null
  discountAmount?: number | null
}

type DiscountValidation = {
  valid: boolean
  discountType?: string
  discountAmount?: number
  percentageValue?: number
  error?: string
}

function formatMoney(amount: number): string {
  return amount.toLocaleString('ru-RU', { style: 'currency', currency: 'RUB', minimumFractionDigits: 0, maximumFractionDigits: 0 })
}

function trackFbq(eventName: string, params: Record<string, unknown>, eventId?: string) {
  const fbq = (window as unknown as { fbq?: (...args: unknown[]) => void }).fbq
  if (!fbq) return
  if (eventId) {
    fbq('track', eventName, params, { eventID: eventId })
  } else {
    fbq('track', eventName, params)
  }
}

function ChecklistItem({ label, status }: { label: string; status: 'pending' | 'loading' | 'completed' }) {
  return (
    <div className="flex items-center gap-3 py-2">
      <div className={cn(
        "flex h-6 w-6 shrink-0 items-center justify-center rounded-full border transition-all duration-500",
        status === 'completed' ? "bg-green-500 border-green-500 text-white" :
        status === 'loading' ? "border-[var(--theme-accent)] text-[var(--theme-accent-text)]" :
        "border-[var(--theme-accent-soft)] text-[var(--theme-accent-soft)] bg-white"
      )}>
        {status === 'completed' ? <Check className="h-3.5 w-3.5" /> :
         status === 'loading' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> :
         <div className="h-1.5 w-1.5 rounded-full bg-current" />}
      </div>
      <span className={cn(
        "text-sm font-medium transition-colors duration-300",
        status === 'completed' ? "text-gray-900" :
        status === 'loading' ? "text-[var(--theme-accent-text)]" :
        "text-gray-500"
      )}>{label}</span>
    </div>
  )
}

function getNotificationText(order: OrderSummary | null): string {
  const hasEmail = !!order?.customer?.email
  if (hasEmail) return 'Заказ принят. Песню отправим на электронную почту, когда будет готова.'
  return 'Заказ принят. Страница обновится автоматически, когда песня будет готова.'
}

export function CheckoutRoute() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const orderId = useMemo(() => params.get('orderId') ?? '', [params])
  const discountCodeFromUrl = useMemo(() => params.get('discountCode') ?? '', [params])

  const [loading, setLoading] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [order, setOrder] = useState<OrderSummary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [checklistStep, setChecklistStep] = useState(0)
  const [manualConfirmationEnabled, setManualConfirmationEnabled] = useState<boolean | null>(null)

  const hasAttemptedAutoConfirm = useRef(false)
  const hasTrackedInitiateCheckout = useRef(false)
  const [themeColors, setThemeColors] = useState<{ accentColor?: string; buttonColor?: string; bgColor1?: string; bgColor2?: string } | null | undefined>(undefined)

  const [paymentAmount, setPaymentAmount] = useState<number>(2990)
  const [discountValidation, setDiscountValidation] = useState<DiscountValidation | null>(null)
  const [discountLoading, setDiscountLoading] = useState(false)
  const [applyingDiscount, setApplyingDiscount] = useState(false)
  const [initiatingPayment, setInitiatingPayment] = useState(false)
  const [widgetToken, setWidgetToken] = useState<string | null>(null)
  const tokenRetriedRef = useRef(false)

  // Тот же адрес, что бэкенд ставит redirect-платежу (server.go), собран из
  // orderId, полученного от сервера (query /checkout), а не из своего состояния —
  // после T-004 сюда придёт подписанный ref, и код не придётся править.
  const returnUrl = useMemo(
    () => `${window.location.origin}/thanks?orderId=${encodeURIComponent(orderId)}`,
    [orderId],
  )

  // Запрашивает embedded-токен виджета. Если сервер вместо токена отдаёт
  // confirmationUrl (мок реализует только эту ветку) или заказ уже оплачен —
  // ведёт себя так же, как раньше (редирект / обновление статуса заказа).
  const requestEmbeddedPayment = useCallback(async () => {
    setInitiatingPayment(true)
    setError(null)
    try {
      const res = await apiPost<{ confirmationToken?: string; confirmationUrl?: string; paid?: boolean }>(
        `/api/orders/${encodeURIComponent(orderId)}/initiate-payment`,
        { mode: 'embedded' },
      )
      if (res.paid) {
        const refreshed = await apiGet<OrderSummary>(`/api/orders/${encodeURIComponent(orderId)}`)
        setOrder(refreshed)
        setInitiatingPayment(false)
      } else if (res.confirmationToken) {
        setWidgetToken(res.confirmationToken)
        setInitiatingPayment(false)
      } else if (res.confirmationUrl) {
        window.location.href = res.confirmationUrl
      } else {
        setInitiatingPayment(false)
      }
    } catch (err) {
      setInitiatingPayment(false)
      throw err
    }
  }, [orderId])

  // Штатный фолбэк: виджет не смог отрисоваться/отработать — уходим на
  // страницу ЮKassa по ссылке (redirect), как было до виджета. Остаётся
  // рабочим путём навсегда.
  const fallbackToRedirect = useCallback(async () => {
    setWidgetToken(null)
    setInitiatingPayment(true)
    try {
      const r = await apiPost<{ confirmationUrl?: string }>(
        `/api/orders/${encodeURIComponent(orderId)}/initiate-payment`,
        { mode: 'redirect' },
      )
      if (r.confirmationUrl) {
        window.location.href = r.confirmationUrl
      } else {
        setInitiatingPayment(false)
        setError('Не удалось перейти к оплате. Попробуйте ещё раз.')
      }
    } catch {
      setInitiatingPayment(false)
      setError('Не удалось перейти к оплате. Попробуйте ещё раз.')
    }
  }, [orderId])

  // onFail виджета: скрипт не загрузился, error_callback сработал, либо
  // истёк токен (token_expired, живёт час) — для истёкшего токена одна
  // попытка перевыпустить его свежим, и только затем фолбэк на redirect.
  const handleWidgetFail = useCallback(async (reason: string) => {
    setWidgetToken(null)
    if (reason === 'token_expired' && !tokenRetriedRef.current) {
      tokenRetriedRef.current = true
      try {
        await requestEmbeddedPayment()
        return
      } catch {
        // не получилось перевыпустить токен — уходим на redirect ниже
      }
    }
    await fallbackToRedirect()
  }, [requestEmbeddedPayment, fallbackToRedirect])

  useEffect(() => {
    if (!order) return
    let cancelled = false
    const themeParam = order.themeSlug ? `?theme=${encodeURIComponent(order.themeSlug)}` : ''
    apiGet<{ manualConfirmationEnabled?: boolean; paymentAmount?: number; colors?: { accentColor?: string; buttonColor?: string; bgColor1?: string; bgColor2?: string } }>(`/api/public/settings${themeParam}`)
      .then((res) => {
        if (cancelled) return
        setManualConfirmationEnabled(Boolean(res?.manualConfirmationEnabled))
        setPaymentAmount(res?.paymentAmount ?? 2990)
        const c = (res as Loose)?.publicSiteConfig?.colors ?? (res as Loose)?.colors
        setThemeColors(c ?? null)
      })
      .catch(() => {
        if (!cancelled) {
          setManualConfirmationEnabled(false)
          setThemeColors(null)
        }
      })
    return () => {
      cancelled = true
    }
  }, [order?.themeSlug])

  const themeStyle = {
    '--theme-accent': themeColors?.accentColor || '#E11D48',
    '--theme-button': themeColors?.buttonColor || themeColors?.accentColor || '#E11D48',
    '--theme-accent-soft': themeColors?.bgColor1 || '#FFF5F7',
    '--theme-bg': themeColors?.bgColor2 || '#FFFFFF',
  } as React.CSSProperties

  // Meta Pixel: track InitiateCheckout when the checkout page is loaded.
  // Guard avoids double fire in dev StrictMode / re-mounts.
  useEffect(() => {
    if (!orderId) return
    if (hasTrackedInitiateCheckout.current) return
    hasTrackedInitiateCheckout.current = true
    trackFbq('InitiateCheckout', {
      value: paymentAmount,
      currency: 'RUB',
      num_items: 1,
      content_type: 'product',
    })
    trackTikTokEvent('InitiateCheckout', {
      value: paymentAmount,
      currency: 'RUB',
    })
  }, [orderId])

  const needsXenditPayment = order?.paymentStatus === 'pending' && order?.xenditInvoiceUrl
  const needsPaymentInit = order?.paymentStatus === 'pending' && !order?.xenditInvoiceUrl
  const paymentNotReady = order?.paymentStatus !== 'paid' && order?.paymentStatus !== 'free'

  useEffect(() => {
    if (manualConfirmationEnabled) return
    if (needsXenditPayment || paymentNotReady) return
    if (orderId && order?.status === 'created' && !confirming && !hasAttemptedAutoConfirm.current) {
      hasAttemptedAutoConfirm.current = true
      confirm()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderId, order?.status, manualConfirmationEnabled, needsXenditPayment, paymentNotReady])

  // Initial Load
  useEffect(() => {
    if (!orderId) return
    setLoading(true)
    setError(null)
    apiGet<OrderSummary>(`/api/orders/${encodeURIComponent(orderId)}`)
      .then(setOrder)
      .catch((e) => setError(e?.message ?? 'Не удалось загрузить заказ'))
      .finally(() => setLoading(false))
  }, [orderId])

  // Auto-validate discount code from URL
  useEffect(() => {
    if (!discountCodeFromUrl || !order) return
    if (order.discountCode === discountCodeFromUrl.toUpperCase()) return
    if (order.paymentStatus === 'paid') return

    setDiscountLoading(true)
    apiPost<DiscountValidation>('/api/public/discount/validate', {
      code: discountCodeFromUrl,
      templateSlug: order.themeSlug ?? undefined,
      phoneNumber: undefined,
    })
      .then(setDiscountValidation)
      .catch(() => setDiscountValidation({ valid: false, error: 'Не удалось проверить промокод.' }))
      .finally(() => setDiscountLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [discountCodeFromUrl, order?.id, order?.discountCode, order?.paymentStatus])

  // Calculate display prices
  const displayBasePrice = order?.basePrice ?? paymentAmount
  const calculatedDiscount = useMemo(() => {
    if (order?.discountCode && order?.discountAmount) {
      return order.discountAmount
    }
    if (discountValidation?.valid && displayBasePrice) {
      if (discountValidation.discountType === 'percentage' && discountValidation.percentageValue) {
        return Math.floor(displayBasePrice * discountValidation.percentageValue / 100)
      }
      return discountValidation.discountAmount ?? 0
    }
    return 0
  }, [order?.discountCode, order?.discountAmount, discountValidation, displayBasePrice])
  const displayFinalPrice = Math.max(0, displayBasePrice - calculatedDiscount)

  // Poll for updates if processing (for checklist sync mostly)
  useEffect(() => {
    if (!orderId || !order || (order.status !== 'processing' && order.status !== 'created')) return

    const handle = window.setInterval(() => {
      apiGet<OrderSummary>(`/api/orders/${encodeURIComponent(orderId)}`)
        .then((o) => {
          setOrder(o)
        })
        .catch(() => {})
    }, 3000)

    return () => window.clearInterval(handle)
  }, [orderId, order?.status])

  // Animation Logic
  useEffect(() => {
    if (!order) return
    
    // Reset if just created and not confirming
    if (order.status === 'created' && !confirming) {
      setChecklistStep(0)
    }

    // For best UX, if backend marks it failed, keep a "completed" looking checklist.
    if (order.status === 'failed') {
      setChecklistStep(3)
      return
    }

    // If confirming, move to step 1 (Pesanan Diterima: Done, Verifikasi: Loading)
    if (confirming && checklistStep === 0) {
      setChecklistStep(1)
    }

    // If processing, advance steps
    if (order.status === 'processing') {
      if (checklistStep === 0) setChecklistStep(1) // Catch up if needed
      
      if (checklistStep === 1) {
        const timer = setTimeout(() => setChecklistStep(2), 1000)
        return () => clearTimeout(timer)
      } else if (checklistStep === 2) {
        const timer = setTimeout(() => setChecklistStep(3), 2500)
        return () => clearTimeout(timer)
      }
    }
  }, [order?.status, confirming, checklistStep])


  async function confirm() {
    if (!orderId) return
    setConfirming(true)
    setError(null)
    try {
      await apiPost(`/api/orders/${encodeURIComponent(orderId)}/confirm`, {})

      // Note: Purchase tracking (pixel + CAPI) is now handled on the delivery page
      // to ensure it fires exactly once on first visit.

      const refreshed = await apiGet<OrderSummary>(`/api/orders/${encodeURIComponent(orderId)}`)
      setOrder(refreshed)
      // Animation will trigger via effect
    } catch {
      // Never show technical errors to end users on this route.
      setError('Заказ уже принят. Подождите, мы его обрабатываем.')
    } finally {
      setConfirming(false)
    }
  }

  const showProgressUI = Boolean(order && !needsXenditPayment && !needsPaymentInit && order.paymentStatus !== 'failed' && (order.status === 'processing' || order.status === 'created' || order.status === 'failed' || confirming || hasAttemptedAutoConfirm.current))

  return (
    <div className="min-h-screen bg-[var(--theme-accent-soft)] font-sans pb-32 pt-10 px-4" style={themeStyle}>
      <div className="mx-auto max-w-lg space-y-8">
        
        {/* Header */}
        <div className="text-center space-y-2">
           <h1 className="font-serif text-3xl font-bold text-gray-900 flex justify-center items-center gap-2">
             <Music className="text-[var(--theme-accent-text)] h-8 w-8" /> Оплата
           </h1>
           <p className="text-gray-500 text-sm">Проверьте заказ перед созданием песни.</p>
        </div>

        {!orderId ? (
          <p className="text-center text-red-500">Не указан номер заказа.</p>
        ) : loading ? (
          <div className="flex justify-center py-10">
            <Loader2 className="h-8 w-8 animate-spin text-[var(--theme-accent-text)]" />
          </div>
        ) : order ? (
          <div className="space-y-6">

            {needsXenditPayment && (
              <Card className="border-[var(--theme-accent-soft)] shadow-xl bg-white/80 backdrop-blur-sm overflow-hidden animate-in fade-in zoom-in duration-300">
                <div className="h-1.5 bg-[var(--theme-accent)] w-full" />
                <CardContent className="p-6 space-y-4 text-center">
                  <CreditCard className="h-10 w-10 mx-auto text-[var(--theme-accent-text)]" />
                  <h3 className="font-bold text-gray-900 text-lg">Требуется оплата</h3>

                  {/* Price Display */}
                  <div className="space-y-1 py-1">
                    {calculatedDiscount > 0 ? (
                      <>
                        <div className="text-sm text-gray-400 line-through">
                          {formatMoney(displayBasePrice)}
                        </div>
                        <div className="text-2xl font-bold text-[var(--theme-accent-text)]">
                          {formatMoney(displayFinalPrice)}
                        </div>
                        <div className="inline-flex items-center gap-1 bg-green-50 text-green-700 text-xs font-medium px-2 py-0.5 rounded-full">
                          Скидка {formatMoney(calculatedDiscount)}
                        </div>
                      </>
                    ) : (
                      <div className="text-2xl font-bold text-gray-900">
                        {formatMoney(displayBasePrice)}
                      </div>
                    )}
                  </div>

                  {discountLoading && (
                    <div className="flex items-center justify-center gap-2 text-sm text-gray-500">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Проверка промокода…
                    </div>
                  )}
                  {discountValidation && !discountValidation.valid && (
                    <div className="text-sm text-red-500 bg-red-50 px-3 py-2 rounded-lg">
                      {discountValidation.error || 'Промокод недействителен.'}
                    </div>
                  )}

                  <p className="text-sm text-gray-600">
                    Перейдите к оплате, чтобы мы начали работу. После успешной оплаты генерация запускается автоматически.
                  </p>
                  <Button
                    className="w-full h-14 rounded-full bg-[var(--theme-button)] text-white text-lg font-bold shadow-xl hover:opacity-90 hover:scale-[1.02] transition-all duration-300"
                    disabled={applyingDiscount || discountLoading}
                    onClick={async () => {
                      // If there's a valid discount from URL that hasn't been applied yet
                      if (discountCodeFromUrl && discountValidation?.valid && order?.discountCode !== discountCodeFromUrl.toUpperCase()) {
                        setApplyingDiscount(true)
                        setError(null)
                        try {
                          const result = await apiPost<{
                            ok: boolean
                            paymentAmount: number
                            xenditInvoiceUrl: string | null
                          }>(`/api/orders/${encodeURIComponent(orderId)}/apply-discount`, {
                            discountCode: discountCodeFromUrl,
                          })
                          if (result.xenditInvoiceUrl) {
                            window.location.href = result.xenditInvoiceUrl
                          } else {
                            // Free order (discount covers full amount)
                            const refreshed = await apiGet<OrderSummary>(
                              `/api/orders/${encodeURIComponent(orderId)}`
                            )
                            setOrder(refreshed)
                          }
                        } catch (err) {
                          setError(err instanceof Error ? err.message : 'Не удалось применить промокод.')
                          setApplyingDiscount(false)
                        }
                      } else if (order?.xenditInvoiceUrl) {
                        window.location.href = order.xenditInvoiceUrl
                      }
                    }}
                  >
                    {applyingDiscount ? (
                      <span className="flex items-center gap-2"><Loader2 className="animate-spin" /> Обработка…</span>
                    ) : (
                      'Оплатить'
                    )}
                  </Button>
                  <p className="text-[10px] text-gray-400">Откроется страница платёжного провайдера (ЮKassa и др.)</p>
                </CardContent>
              </Card>
            )}

            {needsPaymentInit && (
              <Card className="border-[var(--theme-accent-soft)] shadow-xl bg-white/80 backdrop-blur-sm overflow-hidden animate-in fade-in zoom-in duration-300">
                <div className="h-1.5 bg-[var(--theme-accent)] w-full" />
                <CardContent className="p-6 space-y-4 text-center">
                  <CreditCard className="h-10 w-10 mx-auto text-[var(--theme-accent-text)]" />
                  <h3 className="font-bold text-gray-900 text-lg">Оплата заказа</h3>

                  <div className="space-y-1 py-1">
                    {calculatedDiscount > 0 ? (
                      <>
                        <div className="text-sm text-gray-400 line-through">{formatMoney(displayBasePrice)}</div>
                        <div className="text-2xl font-bold text-[var(--theme-accent-text)]">{formatMoney(displayFinalPrice)}</div>
                        <div className="inline-flex items-center gap-1 bg-green-50 text-green-700 text-xs font-medium px-2 py-0.5 rounded-full">
                          Скидка {formatMoney(calculatedDiscount)}
                        </div>
                      </>
                    ) : (
                      <div className="text-2xl font-bold text-gray-900">{formatMoney(displayBasePrice)}</div>
                    )}
                  </div>

                  {discountLoading && (
                    <div className="flex items-center justify-center gap-2 text-sm text-gray-500">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      Проверка промокода…
                    </div>
                  )}

                  <p className="text-sm text-gray-600">
                    После оплаты начнём создание вашей песни.
                  </p>
                  {widgetToken ? (
                    <YooKassaWidget
                      confirmationToken={widgetToken}
                      returnUrl={returnUrl}
                      onFail={handleWidgetFail}
                    />
                  ) : (
                    <Button
                      className="w-full h-14 rounded-full bg-[var(--theme-button)] text-white text-lg font-bold shadow-xl hover:opacity-90 hover:scale-[1.02] transition-all duration-300"
                      disabled={initiatingPayment || discountLoading}
                      onClick={async () => {
                        setInitiatingPayment(true)
                        setError(null)
                        try {
                          if (discountCodeFromUrl && discountValidation?.valid && order?.discountCode !== discountCodeFromUrl.toUpperCase()) {
                            await apiPost(`/api/orders/${encodeURIComponent(orderId)}/apply-discount`, {
                              discountCode: discountCodeFromUrl,
                            })
                          }
                          tokenRetriedRef.current = false
                          await requestEmbeddedPayment()
                        } catch (err) {
                          setError(err instanceof Error ? err.message : 'Не удалось инициировать оплату.')
                          setInitiatingPayment(false)
                        }
                      }}
                    >
                      {initiatingPayment ? (
                        <span className="flex items-center gap-2"><Loader2 className="animate-spin" /> Обработка…</span>
                      ) : (
                        'Оплатить'
                      )}
                    </Button>
                  )}
                  <p className="text-[10px] text-gray-400">
                    {widgetToken ? 'Форма оплаты ЮKassa открылась прямо на этой странице' : 'Оплата обрабатывается ЮKassa'}
                  </p>
                </CardContent>
              </Card>
            )}

            {order.paymentStatus === 'paid' && !showProgressUI && (
              <Card className="border-green-200 shadow-xl bg-white/80 backdrop-blur-sm overflow-hidden animate-in fade-in zoom-in duration-300">
                <div className="h-1.5 bg-green-500 w-full" />
                <CardContent className="p-6 space-y-3 text-center">
                  <Check className="h-10 w-10 mx-auto text-green-500" />
                  <h3 className="font-bold text-gray-900 text-lg">Оплата прошла успешно!</h3>
                  <p className="text-sm text-gray-600">
                    {getNotificationText(order)}
                  </p>
                </CardContent>
              </Card>
            )}

            {order.paymentStatus === 'failed' && (
              <Card className="border-red-200 shadow-xl bg-white/80 backdrop-blur-sm overflow-hidden animate-in fade-in zoom-in duration-300">
                <div className="h-1.5 bg-red-500 w-full" />
                <CardContent className="p-6 space-y-4 text-center">
                  <CreditCard className="h-10 w-10 mx-auto text-red-500" />
                  <h3 className="font-bold text-gray-900 text-lg">Оплата не прошла</h3>
                  <p className="text-sm text-gray-600">
                    {order.errorMessage || 'Счёт истёк или оплата отклонена. Оформите новый заказ.'}
                  </p>
                  <Button
                    variant="outline"
                    className="w-full h-12 rounded-full text-base font-semibold"
                    onClick={() => navigate(order.themeSlug ? `/${order.themeSlug}/config` : '/config')}
                  >
                    Новый заказ
                  </Button>
                </CardContent>
              </Card>
            )}
            
            {/* Status Card */}
            {showProgressUI && (
              <Card className="border-[var(--theme-accent-soft)] shadow-xl bg-white/80 backdrop-blur-sm overflow-hidden animate-in fade-in zoom-in duration-300">
                <div className="h-1.5 bg-[var(--theme-accent)] w-full animate-pulse" />
                <CardContent className="p-6 space-y-4">
                  <h3 className="font-bold text-gray-900 text-lg">Статус заказа</h3>
                  <div className="space-y-1 pl-1">
                    <ChecklistItem label="Заказ получен" status={checklistStep >= 1 ? 'completed' : 'pending'} />
                    <ChecklistItem label="Проверка данных" status={checklistStep >= 2 ? 'completed' : checklistStep === 1 ? 'loading' : 'pending'} />
                    <ChecklistItem label="Запуск создания песни" status={checklistStep >= 3 ? 'completed' : checklistStep === 2 ? 'loading' : 'pending'} />
                  </div>
                  
                  <div className={cn(
                    "bg-[var(--theme-accent-soft)] text-[var(--theme-accent-text)] text-sm p-4 rounded-xl flex items-start gap-3 mt-4 border border-[var(--theme-accent-soft)] transition-opacity duration-500",
                    checklistStep >= 3 ? "opacity-100" : "opacity-0"
                  )}>
                    <Clock className="h-5 w-5 shrink-0 mt-0.5 text-[var(--theme-accent-text)]" />
                    <div>
                      <p className="font-bold text-[var(--theme-accent-text)]">Заказ успешно принят</p>
                      <p className="opacity-90 mt-1">
                        {getNotificationText(order)}
                      </p>
                    </div>
                  </div>
                </CardContent>
              </Card>
            )}

            {/* Order Summary */}
            <Card className="border-gray-100 shadow-sm">
              <CardHeader className="pb-3 border-b border-gray-50 bg-gray-50/50">
                <CardTitle className="text-base text-gray-900">Сводка заказа</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4 pt-4 text-sm">
                <div className="grid grid-cols-[100px_1fr] gap-2">
                  <span className="text-gray-500">Для</span>
                  <span className="font-medium text-gray-900">{order.inputPayload.recipientName ?? '-'}</span>
                  
                  <span className="text-gray-500">Повод</span>
                  <span className="font-medium text-gray-900">{order.inputPayload.occasion ?? '-'}</span>
                </div>
                
                <Separator className="bg-gray-100" />
                
                <div className="space-y-1.5">
                  <div className="text-gray-500">История</div>
                  <div className="whitespace-pre-wrap font-medium text-gray-800 bg-gray-50 p-3 rounded-lg border border-gray-100 text-xs leading-relaxed">
                    {order.inputPayload.story ?? '-'}
                  </div>
                </div>

                {order.inputPayload.extraNotes && (
                  <div className="space-y-1.5">
                    <div className="text-gray-500">Дополнительно</div>
                    <div className="whitespace-pre-wrap text-gray-800 text-xs italic">
                      "{order.inputPayload.extraNotes}"
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Actions: Removed auto-confirm button. Only show if error occurs to retry. */}
            {error && order.status === 'created' ? (
               <Button 
                 className="w-full h-14 rounded-full bg-[var(--theme-button)] text-white text-lg font-bold shadow-xl shadow-[var(--theme-accent-soft)] hover:opacity-90 hover:scale-[1.02] transition-all duration-300" 
                 onClick={() => {
                    hasAttemptedAutoConfirm.current = true
                    confirm()
                 }} 
                 disabled={confirming}
               >
                 {confirming ? (
                   <span className="flex items-center gap-2"><Loader2 className="animate-spin" /> Обработка…</span>
                 ) : (
                   'Повторить подтверждение заказа'
                 )}
               </Button>
            ) : null}

          </div>
        ) : null}

        {error ? (
          <div className="bg-[var(--theme-accent-soft)] text-[var(--theme-accent-text)] p-4 rounded-xl text-center text-sm font-medium border border-[var(--theme-accent-soft)]">
            {error}
          </div>
        ) : null}
        
        <div className="text-center pt-8">
           <Button variant="ghost" className="text-gray-400 hover:text-gray-600" onClick={() => navigate('/')}>
             <ArrowLeft className="h-4 w-4 mr-2" /> На главную
           </Button>
        </div>

      </div>
    </div>
  )
}
