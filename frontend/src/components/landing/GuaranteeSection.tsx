import { useEffect, useRef, useState } from 'react'
import { ShieldCheck, Check } from 'lucide-react'
import { useTr } from '@/lib/lang'

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)(?!span\b)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

type Props = {
  deliveryEtaShort: string
  badgeText?: string
  headline?: string
  description?: string
  badges?: string[]
  videoUrl?: string
}

export function GuaranteeSection({
  deliveryEtaShort,
  badgeText: badgeTextProp,
  headline: headlineProp,
  description: descriptionProp,
  badges: badgesProp,
  videoUrl = '/laguin-studio.mp4',
}: Props) {
  const tr = useTr()
  const badgeText = badgeTextProp ?? tr('Без риска', 'No risk')
  const headline = headlineProp ?? tr('Гарантия эмоций', 'Guaranteed to move them')
  const description = descriptionProp ?? tr(
    'Если песня не тронула — <strong class="text-gray-900">переделаем бесплатно</strong> или <strong class="text-gray-900">вернём полную сумму</strong>. Без лишних вопросов.',
    'If the song doesn\'t move them, we\'ll <strong class="text-gray-900">remake it for free</strong> or <strong class="text-gray-900">refund the full amount</strong>. No questions asked.',
  )
  const badges = badgesProp ?? [tr('Правки бесплатно', 'Free revisions'), tr('Полный возврат', 'Full refund')]
  const videoRef = useRef<HTMLDivElement>(null)
  const [videoVisible, setVideoVisible] = useState(false)

  useEffect(() => {
    const el = videoRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setVideoVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '200px' }
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  return (
    <section className="max-w-3xl mx-auto">
      <div className="bg-[#ECFDF5] border border-[#D1FAE5] rounded-3xl p-8 md:p-12 text-center relative overflow-hidden">
        <div className="absolute top-0 right-0 p-4 opacity-10">
          <ShieldCheck className="w-32 h-32 text-green-600" />
        </div>
        <div className="relative z-10 space-y-4">
          <div className="inline-flex items-center gap-2 bg-white px-4 py-2 rounded-full text-green-700 font-bold text-sm shadow-sm mb-2">
            <ShieldCheck className="h-4 w-4" /> {badgeText}
          </div>
          <h3 className="font-serif text-3xl font-bold text-gray-900">{headline}</h3>
          <p className="text-gray-700 max-w-lg mx-auto" dangerouslySetInnerHTML={{ __html: sanitizeHtml(description) }} />
          <div className="flex flex-wrap justify-center gap-6 text-sm font-bold text-green-700 pt-2">
            {badges.map((badge, i) => (
              <span key={i} className="flex items-center gap-1"><Check className="h-4 w-4" /> {badge}</span>
            ))}
            <span className="flex items-center gap-1"><Check className="h-4 w-4" /> {tr(`Доставка ${deliveryEtaShort}`, `Delivered in ${deliveryEtaShort}`)}</span>
          </div>
        </div>
      </div>
      <div className="mt-6" ref={videoRef}>
        {videoVisible && (
          <video
            src={videoUrl}
            className="w-full rounded-2xl shadow-md"
            autoPlay
            muted
            loop
            playsInline
          />
        )}
      </div>
    </section>
  )
}
