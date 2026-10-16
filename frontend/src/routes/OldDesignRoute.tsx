import { useLandingData } from '@/hooks/useLandingData'
import type { LandingData } from '@/hooks/useLandingData'
import { lazy, Suspense } from 'react'
import type { TemplateDef } from '@/templates/types'

// Lazy-load grace template to keep it out of the main bundle.
// Grace is only used on /old-design route and should not bloat the landing's startup JS.
const GraceRenderer = lazy(() =>
  import('@/templates/grace').then(m => {
    const graceDef = m.default as TemplateDef
    return {
      default: (props: LandingData) => <graceDef.Render {...{ ...props, design: 'grace' }} />,
    }
  }),
)

export function OldDesignRoute() {
  const data = useLandingData()
  // Until /settings resolves the template, paint the hero poster immediately
  // (it's preloaded in index.html) so the LCP/FCP doesn't wait on the API.
  if (!data.design) {
    // Mirrors the static hero in index.html (eyebrow + poster, same paddings)
    // so static HTML → this placeholder → real grace hero transition has no shift.
    return (
      <div className="min-h-screen" style={{ backgroundColor: '#FDFBF7' }}>
        <p
          className="text-center text-xs font-bold uppercase tracking-widest px-4 pt-4"
          style={{ color: '#B5952F' }}
        >
          Подарок, который трогает душу
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
  // Render grace template via lazy-loaded component (excluded from eager glob to save LCP)
  return (
    <Suspense fallback={null}>
      <GraceRenderer {...data} />
    </Suspense>
  )
}
