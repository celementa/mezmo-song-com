import { apiGet } from '@/lib/http'
import { getLang } from '@/lib/lang'

import { executePixelScript } from './MetaPixel'
import { executeTikTokScript } from './TikTokPixel'

export type PurchaseTrackingOrderStatus = {
  id: string // публичный ref — только для URL (mark-purchase-tracked)
  trackingId?: string // сырой id заказа — для transaction_id/eventID (T-005)
  status: string
  paymentStatus?: string | null
  purchaseTrackedAt?: string | null
  isTest?: boolean
  paymentAmount?: number | null
  pixelConfirmScript?: string | null
  tiktokPixelConfirmScript?: string | null
}

/**
 * Отбивает покупку в браузере ровно один раз на заказ: пушит `purchase`
 * в dataLayer (оттуда теги контейнера собирают цель и ecommerce Яндекс.Метрики)
 * и выполняет админские пиксельные скрипты. GA4 и VK сюда не относятся — они
 * уходят серверной шиной с вебхука ЮKassa.
 *
 * Порядок операций важен: сначала **заявка** на сервер, и только на ответ
 * `first: true` уходит событие. Обратный порядок (событие, затем отметка
 * вдогонку) давал гонку — переход с /thanks на страницу заказа успевал
 * прочитать ещё пустой `purchaseTrackedAt` и отправить покупку второй раз.
 * Арбитр один — атомарный UPDATE в `store.MarkPurchaseTracked`, поэтому
 * сколько бы точек трекинга, вкладок и устройств ни было, событие уйдёт однажды.
 *
 * Возвращает true, только если событие действительно отправлено.
 */
export async function fireOrderPurchaseTracking(
  orderStatus: PurchaseTrackingOrderStatus,
  opts?: { allowTest?: boolean },
): Promise<boolean> {
  // Тестовые заказы из /admin/ads (рекламный доступ, DiscountCode=INTERNAL) не должны
  // «отбивать» покупку — если тоггл «Трекать тестовые оплаты» в админке выключен.
  if (orderStatus.isTest && !opts?.allowTest) return false
  // Only fire for confirmed orders (not 'created' — wait for auto-confirm to complete)
  if (orderStatus.status === 'created') return false
  // Never fire purchase tracking for unpaid orders
  if (orderStatus.paymentStatus === 'pending') return false
  // Уже отбито (этой или другой страницей/устройством) — не идём даже за заявкой.
  if (orderStatus.purchaseTrackedAt) return false

  const orderId = orderStatus.id
  // Сырой id заказа — общий ключ покупки для браузерных и серверных событий.
  // Серверные синки (sendGA4Purchase/sendVKConversion) отправляют именно его,
  // поэтому ref сюда подставлять нельзя: одна покупка получит два разных
  // transaction_id и не сойдётся ни в GA4, ни с заказом в админке.
  const trackingId = orderStatus.trackingId ?? orderStatus.id

  // Read Meta cookies client-side and forward them explicitly so the server
  // always has fbp/fbc even if cookie headers are stripped by a proxy.
  //
  // Внимание: `fbp`/`fbc` тело запроса содержит, но сервер их НЕ читает —
  // handleMarkPurchaseTracked только вызывает store.MarkPurchaseTracked.
  // Серверных отправок в Meta CAPI и TikTok Events API в проекте нет вообще
  // (проверено по коду и истории git 2026-08-10). Поля оставлены на случай,
  // если CAPI когда-нибудь появится, — тогда менять фронт не придётся.
  const cookies = document.cookie.split(';').reduce<Record<string, string>>((acc, c) => {
    const [k, ...v] = c.split('=')
    if (k) acc[k.trim()] = v.join('=').trim()
    return acc
  }, {})

  // Заявка. Сетевая ошибка или 500 → событие НЕ отправляем: отметка на сервере
  // осталась пустой, и следующее открытие страницы заказа попробует снова.
  // Потерять одну попытку безопаснее, чем задвоить покупку в отчётах.
  let first = false
  try {
    const res = await fetch(`/api/public/order/${encodeURIComponent(orderId)}/mark-purchase-tracked`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ fbp: cookies._fbp || undefined, fbc: cookies._fbc || undefined }),
    })
    if (!res.ok) return false
    const claim = (await res.json()) as { first?: boolean }
    first = claim.first === true
  } catch {
    return false
  }
  if (!first) return false

  // Push GTM `purchase` event into the dataLayer (GA4-формат: transaction_id/value/items).
  // Ecommerce для Метрики строит тег контейнера «cHTML - YM ecom - purchase» из items —
  // свой ecommerce-пуш здесь НЕ нужен, иначе Метрика посчитает покупку дважды.
  const price = orderStatus.paymentAmount ?? 0
  const productId = 'song_standard'
  const productName = 'Персональная песня — Стандарт'
  let customerType: 'new' | 'returning' = 'new'
  try {
    customerType = localStorage.getItem('purchase_made') === '1' ? 'returning' : 'new'
    localStorage.setItem('purchase_made', '1')
  } catch { /* localStorage unavailable */ }

  const dataLayer = ((window as unknown as { dataLayer?: Record<string, unknown>[] }).dataLayer ??= [])
  dataLayer.push({
    event: 'purchase',
    transaction_id: trackingId,
    value: price,
    currency: 'USD',
    customer_type: customerType,
    language: getLang(),
    items: [{ item_id: productId, item_name: productName, price, quantity: 1, item_category: 'Song' }],
  })

  // GTM грузится отложенно (см. index.html: по взаимодействию или через 3.5с).
  // Покупка — критичное событие: форсируем загрузку контейнера немедленно, чтобы
  // событие не потерялось, если человек закроет вкладку раньше таймера.
  ;(window as unknown as { __gtmLoad?: () => void }).__gtmLoad?.()

  // Fire admin-configured custom pixel script with dynamic order value
  // Admin scripts can use: orderValue, currency, eventID variables
  const eventId = `order_confirmed:${trackingId}`
  const pixelVars = { eventID: eventId, orderValue: price, currency: 'USD' }
  if (orderStatus.pixelConfirmScript) {
    executePixelScript(orderStatus.pixelConfirmScript, pixelVars)
  }
  if (orderStatus.tiktokPixelConfirmScript) {
    executeTikTokScript(orderStatus.tiktokPixelConfirmScript, pixelVars)
  }

  return true
}

/**
 * Обёртка для страниц: подтягивает тоггл «трекать тестовые оплаты» и отбивает
 * покупку. Точек вызова две — /thanks (return_url) и страница заказа.
 *
 * Вторая точка нужна из-за T-012: с виджетом ЮKassa на своём домене покупатель
 * никуда не уходит и потому не «возвращается» — СБП уводит его в банковское
 * приложение, редирект на return_url не срабатывает, и /thanks не открывается.
 * По логам с 06.08.2026 браузерных отбивок не было ни одной при 14 оплатах,
 * тогда как страницу заказа (ссылка в письме) открывают штатно. Дубля нет:
 * `first: true` сервер отдаёт ровно одной точке.
 */
export async function runOrderPurchaseTracking(
  orderStatus: PurchaseTrackingOrderStatus,
): Promise<boolean> {
  // Тоггл имеет смысл только для тестовых заказов — для клиентских не спрашиваем,
  // чтобы не добавлять запрос на каждое открытие страницы заказа.
  let allowTest = false
  if (orderStatus.isTest) {
    try {
      // Напрямую в API, мимо cachedSettingsGet: тот отдаёт инлайн-блоб
      // window.__SETTINGS__, в котором нет trackTestOrders (тоггл меняется
      // на лету и должен читаться свежим).
      const settings = await apiGet<{ trackTestOrders?: boolean }>('/api/public/settings')
      allowTest = Boolean(settings.trackTestOrders)
    } catch { /* настройки недоступны — тестовые не трекаем */ }
  }
  return fireOrderPurchaseTracking(orderStatus, { allowTest })
}
