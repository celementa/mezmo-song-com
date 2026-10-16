import { useLandingData } from '@/hooks/useLandingData'
import { getTemplate } from '@/templates'

export function LandingRoute() {
  const data = useLandingData()
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
  const { Render } = getTemplate(data.design)
  return <Render {...data} />
}
