import { useLocation } from 'react-router-dom'

import { useLandingData } from '@/hooks/useLandingData'
import { usePricesOnRoot } from '@/hooks/usePricesOnRoot'
import { stripLang, useTr } from '@/lib/lang'
import { getTemplate } from '@/templates'

export function LandingRoute() {
  const { pathname } = useLocation()
  // Главная и /start — две версии одного лендинга: одна с ценами, другая без.
  // Какая из них какая, решает настройка «Цены на главной» в админке.
  const pricesOnRoot = usePricesOnRoot()
  const hidePrices = stripLang(pathname) === '/start' ? pricesOnRoot : !pricesOnRoot
  const data = useLandingData(hidePrices ? { hidePrices: true } : undefined)
  const tr = useTr()
  // Until /settings resolves the template, paint the hero poster immediately
  // (it's preloaded in index.html) so the LCP/FCP doesn't wait on the API.
  if (!data.design) {
    // Mirrors the static hero in index.html (eyebrow + poster, same paddings)
    // so static HTML → this placeholder → real hero transition has no shift.
    return (
      <div className="min-h-screen" style={{ backgroundColor: '#FBF7F1' }}>
        <p
          className="text-center text-xs font-bold uppercase tracking-widest px-4 pt-4"
          style={{ color: '#B5952F' }}
        >
          {tr('Подарок, который трогает душу', 'A gift that touches the heart')}
        </p>
        <div className="px-4 pt-4">
          <div
            className="relative w-full overflow-hidden bg-gray-900 mx-auto"
            style={{ aspectRatio: '16/9', maxWidth: '36rem' }}
          >
            <img
              src="/images/hero-poster.jpg"
              alt=""
              fetchPriority="high"
              className="absolute inset-0 h-full w-full object-cover"
            />
          </div>
        </div>
      </div>
    )
  }
  const { Render } = getTemplate(data.design)
  return <Render {...data} />
}
