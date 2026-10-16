import { useEffect, useMemo, useRef, useState } from 'react'

import { cn } from '@/lib/utils'
import type { Loose } from '@/lib/loose'

export type ActivityToastItem = {
  fullName: string
  city: string
  recipientName: string
}

export type ActivityToastConfig = {
  enabled?: boolean
  intervalMs?: number
  durationMs?: number
  items?: ActivityToastItem[]
}

function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n))
}

const TOAST_ORDER_STORAGE_KEY = 'laguin:activity_toast:order:v1'

function fnv1a32(input: string) {
  // FNV-1a 32-bit
  let hash = 0x811c9dc5
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  // Convert to unsigned 32-bit
  return hash >>> 0
}

function getRandomInt(maxExclusive: number) {
  if (maxExclusive <= 0) return 0
  // Prefer crypto for better randomness (available in modern browsers).
  try {
    const g = globalThis.crypto
    if (g && 'getRandomValues' in g) {
      const buf = new Uint32Array(1)
      g.getRandomValues(buf)
      return buf[0] % maxExclusive
    }
  } catch {
    // ignore
  }
  return Math.floor(Math.random() * maxExclusive)
}

function shuffleOrder(n: number) {
  const arr = Array.from({ length: n }, (_, i) => i)
  for (let i = n - 1; i > 0; i--) {
    const j = getRandomInt(i + 1)
    ;[arr[i], arr[j]] = [arr[j], arr[i]]
  }
  return arr
}

function ordersEqual(a: number[] | null | undefined, b: number[] | null | undefined) {
  if (!a || !b) return false
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false
  return true
}

function readStoredOrder(itemsHash: number, expectedLen: number): number[] | null {
  try {
    const raw = window.localStorage.getItem(TOAST_ORDER_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Loose
    if (!parsed || parsed.v !== 1) return null
    if (typeof parsed.hash !== 'number' || parsed.hash !== itemsHash) return null
    if (!Array.isArray(parsed.order)) return null
    const order = parsed.order.filter((x: unknown) => typeof x === 'number' && Number.isFinite(x))
    if (order.length !== expectedLen) return null
    // Ensure it is a permutation of 0..n-1
    const seen = new Set<number>()
    for (const x of order) {
      const xi = Math.floor(x)
      if (xi < 0 || xi >= expectedLen) return null
      if (seen.has(xi)) return null
      seen.add(xi)
    }
    return order as number[]
  } catch {
    return null
  }
}

function storeOrder(itemsHash: number, order: number[]) {
  try {
    const payload = { v: 1, hash: itemsHash, order, updatedAt: Date.now() }
    window.localStorage.setItem(TOAST_ORDER_STORAGE_KEY, JSON.stringify(payload))
  } catch {
    // Best-effort only
  }
}

function makeNextOrder(n: number, prevOrder: number[] | null) {
  if (n <= 1) return Array.from({ length: n }, (_, i) => i)
  // Try a few times to avoid repeating the same order across refresh / cycle.
  for (let attempt = 0; attempt < 6; attempt++) {
    const next = shuffleOrder(n)
    if (!ordersEqual(prevOrder, next)) return next
  }
  // Extremely unlikely fallback: rotate by 1 so it differs.
  if (prevOrder && prevOrder.length === n) return [...prevOrder.slice(1), prevOrder[0]]
  return shuffleOrder(n)
}

export function ActivityToast({ config }: { config?: ActivityToastConfig | null }) {
  const items = useMemo(() => (Array.isArray(config?.items) ? config!.items! : []), [config])
  const enabled = Boolean(config?.enabled) && items.length > 0

  const intervalMs = useMemo(() => clamp(Number(config?.intervalMs ?? 10000), 2000, 120000), [config])
  const durationMs = useMemo(() => clamp(Number(config?.durationMs ?? 4500), 1000, intervalMs - 250), [config, intervalMs])

  const [order, setOrder] = useState<number[]>([])
  const orderRef = useRef<number[]>([])
  const [pos, setPos] = useState(0)
  const posRef = useRef(0)
  const [visible, setVisible] = useState(false)

  const hideTimeoutRef = useRef<number | null>(null)
  const intervalRef = useRef<number | null>(null)
  const startTimeoutRef = useRef<number | null>(null)

  const itemsHash = useMemo(() => {
    // Hash by content (trimmed) so reorder/refresh isn't tied to array identity.
    const sig = items
      .map((x) => `${String(x.fullName ?? '').trim()}|${String(x.city ?? '').trim()}|${String(x.recipientName ?? '').trim()}`)
      .join('||')
    return fnv1a32(sig)
  }, [items])

  // Create a randomized order that does NOT follow JSON order.
  // Also persist last order so refresh won't repeat the same sequence.
  useEffect(() => {
    if (!enabled) {
      setVisible(false)
      setOrder([])
      orderRef.current = []
      setPos(0)
      posRef.current = 0
      return
    }

    const prev = readStoredOrder(itemsHash, items.length)
    const next = makeNextOrder(items.length, prev)
    orderRef.current = next
    setOrder(next)
    posRef.current = 0
    setPos(0)
    storeOrder(itemsHash, next)
  }, [enabled, itemsHash, items.length])

  useEffect(() => {
    if (!enabled || orderRef.current.length === 0) return

    function clearTimers() {
      if (hideTimeoutRef.current) window.clearTimeout(hideTimeoutRef.current)
      if (intervalRef.current) window.clearInterval(intervalRef.current)
      if (startTimeoutRef.current) window.clearTimeout(startTimeoutRef.current)
      hideTimeoutRef.current = null
      intervalRef.current = null
      startTimeoutRef.current = null
    }

    function showCurrentThenHide() {
      setVisible(true)
      if (hideTimeoutRef.current) window.clearTimeout(hideTimeoutRef.current)
      hideTimeoutRef.current = window.setTimeout(() => setVisible(false), durationMs)
    }

    clearTimers()
    posRef.current = 0
    setPos(0)

    // Show first toast shortly after load.
    startTimeoutRef.current = window.setTimeout(() => {
      showCurrentThenHide()
    }, 1500)

    intervalRef.current = window.setInterval(() => {
      const n = orderRef.current.length
      if (!n) return

      let nextPos = posRef.current + 1
      if (nextPos >= n) {
        // New randomized cycle (avoid repeating the same order).
        const nextOrder = makeNextOrder(n, orderRef.current)
        orderRef.current = nextOrder
        setOrder(nextOrder)
        storeOrder(itemsHash, nextOrder)
        nextPos = 0
      }

      posRef.current = nextPos
      setPos(nextPos)
      showCurrentThenHide()
    }, intervalMs)

    return () => clearTimers()
  }, [enabled, intervalMs, durationMs, itemsHash])

  const idx = enabled && order.length ? order[pos % order.length] : null
  const item = idx !== null && idx >= 0 && idx < items.length ? items[idx] : null
  if (!enabled || !item) return null

  return (
    <div
      className={cn(
        // Sembunyikan di layar kecil; tampilkan dari sm (640px) ke atas
        'hidden sm:block',
        // Fix overlap: z-index higher than header (z-50), and push top down to clear header height (~100-110px on mobile).
        'fixed z-[100] right-3 top-[110px] md:right-auto md:top-auto md:left-6 md:bottom-6',
        // Avoid toast causing horizontal scroll on tiny screens.
        'max-w-[calc(100vw-1.5rem)]',
      )}
    >
      <div
        className={cn(
          // Responsive width: fill small screens with padding, but cap on larger screens.
          'w-[240px] max-w-[calc(100vw-1.5rem)] md:w-[360px] rounded-2xl border border-[var(--theme-accent-soft)] bg-white/95 backdrop-blur px-3 py-2.5 md:px-4 md:py-3 shadow-xl transition-all duration-300',
          visible ? 'opacity-100 translate-y-0' : 'pointer-events-none opacity-0 translate-y-2',
        )}
      >
        <div className="flex items-start gap-3">
          <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-[var(--theme-accent-soft)] text-[var(--theme-accent-text)]">
            ♪
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-xs font-bold text-gray-900 md:text-sm">{item.fullName}</div>
            <div className="text-[10px] text-gray-500 md:text-xs">{item.city}</div>
            <div className="mt-1 text-[10px] text-gray-700 md:text-xs">
              Заказал(а) песню для{' '}
              <span className="font-semibold text-gray-900">{item.recipientName}</span>
            </div>
          </div>
          <button
            type="button"
            className="ml-1 -mt-0.5 inline-flex h-6 w-6 md:h-7 md:w-7 items-center justify-center rounded-full text-gray-400 hover:bg-gray-50 hover:text-gray-700"
            onClick={() => setVisible(false)}
            aria-label="Закрыть"
          >
            ×
          </button>
        </div>
      </div>
    </div>
  )
}

