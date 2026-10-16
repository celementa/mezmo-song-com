/**
 * «Прослушать» / «Скачать» готовой песни — события email-маркетинга
 * (Unisender, Flow 6). Фоновый пинг: ошибки глотаются, повтор в рамках
 * одной страницы не шлётся — цепочку писем запускает первое нажатие.
 */

import { apiPost } from '@/lib/http'

export type EngagementEvent = 'listen' | 'download'

const sent = new Set<string>()

export function reportEngagement(orderRef: string | null | undefined, event: EngagementEvent): void {
  if (!orderRef) return
  const key = `${orderRef}:${event}`
  if (sent.has(key)) return
  sent.add(key)
  void apiPost(`/api/public/order/${encodeURIComponent(orderRef)}/engagement`, { event }).catch(() => {
    sent.delete(key)
  })
}
