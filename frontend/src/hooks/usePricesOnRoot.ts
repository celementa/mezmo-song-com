import { useEffect, useState } from 'react'
import { apiGet } from '@/lib/http'
import type { Loose } from '@/lib/loose'

// На главной и на /start живёт один и тот же лендинг в двух режимах: с ценами и
// без. Какой режим у главной, решает настройка «Цены на главной» в админке
// (landing.prices_on_root). Снимок window.__SETTINGS__ вшивается при сборке и
// даёт значение для первого кадра, дальше уточняем по /api/public/settings —
// настройка меняется без пересборки фронта.
const DEFAULT_PRICES_ON_ROOT = true

function inlinedPricesOnRoot(): boolean {
  const inlined = (window as unknown as Loose).__SETTINGS__
  return typeof inlined?.pricesOnRoot === 'boolean' ? inlined.pricesOnRoot : DEFAULT_PRICES_ON_ROOT
}

export function usePricesOnRoot(): boolean {
  const [pricesOnRoot, setPricesOnRoot] = useState(inlinedPricesOnRoot)

  useEffect(() => {
    let alive = true
    apiGet<{ pricesOnRoot?: boolean }>('/api/public/settings')
      .then((data) => {
        if (alive && typeof data.pricesOnRoot === 'boolean') setPricesOnRoot(data.pricesOnRoot)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  return pricesOnRoot
}
