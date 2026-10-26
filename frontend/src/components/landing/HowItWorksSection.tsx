import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { resolveApiUrl } from '@/lib/http'
import { cn } from '@/lib/utils'
import { localePath, useTr } from '@/lib/lang'

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)(?!span\b)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

type StepItem = { icon: string; title: string; desc: string }

type Props = {
  deliveryEtaSentence: string
  fmtCurrency: (v: number | null | undefined) => string
  paymentAmount: number | null
  originalAmount: number | null
  showPriceInButton?: boolean
  themeSlug: string | null
  sectionLabel?: string
  sectionHeadline?: string
  steps?: StepItem[]
  ctaButtonText?: string
  backgroundType?: 'color' | 'image'
  backgroundColor?: string
  backgroundImageUrl?: string
  backgroundOverlayOpacity?: number
  ctaButtonColor?: string
  sectionClassName?: string
  showCtaButton?: boolean
}

export function HowItWorksSection({
  deliveryEtaSentence: _deliveryEtaSentence,
  fmtCurrency,
  paymentAmount,
  originalAmount,
  showPriceInButton = true,
  themeSlug,
  sectionLabel: _sectionLabel = 'Просто',
  sectionHeadline: sectionHeadlineProp,
  steps,
  ctaButtonText: ctaButtonTextProp,
  backgroundType = 'color',
  backgroundColor = '#1a1a2e',
  backgroundImageUrl = '',
  backgroundOverlayOpacity = 0.6,
  ctaButtonColor: _ctaButtonColor,
  sectionClassName,
  showCtaButton = true,
}: Props) {
  const tr = useTr()
  const sectionHeadline = sectionHeadlineProp ?? tr(
    'Три шага к <span class="text-[var(--theme-accent-text)] italic">моменту, который запомнится</span>',
    'Three steps to a <span class="text-[var(--theme-accent-text)] italic">moment they\'ll remember</span>',
  )
  const ctaButtonText = ctaButtonTextProp ?? tr('Создать свою песню', 'Create your song')
  const defaultSteps: StepItem[] = [
    { icon: '✍️', title: tr('Расскажите историю', 'Tell us the story'), desc: '' },
    { icon: '🎵', title: tr('Мы создаём песню', 'We create the song'), desc: '' },
    { icon: '😭', title: tr('Услышьте реакцию', 'Watch their reaction'), desc: '' },
  ]

  const resolvedSteps = steps ?? defaultSteps
  const useImage = backgroundType === 'image' && backgroundImageUrl.trim()

  return (
    <section
      aria-labelledby="how-it-works-title"
      className={cn("relative overflow-hidden sm:rounded-3xl -mx-7 sm:mx-0", sectionClassName)}
      style={!useImage ? { backgroundColor } : undefined}
    >
      {/* Background image layer */}
      {useImage && (
        <img
          src={resolveApiUrl(backgroundImageUrl.trim())}
          alt=""
          aria-hidden
          className="absolute inset-0 h-full w-full object-cover"
        />
      )}

      {/* Dark overlay (for image mode) */}
      {useImage && (
        <div
          className="absolute inset-0"
          style={{ backgroundColor: `rgba(0,0,0,${backgroundOverlayOpacity})` }}
        />
      )}

      {/* Content */}
      <div className="relative z-10 px-6 py-16 sm:px-12 sm:py-20 max-w-2xl mx-auto">
        {/* Headline */}
        <div className="text-center space-y-3 mb-12">
          <h2
            id="how-it-works-title"
            className="font-serif text-3xl sm:text-4xl lg:text-5xl font-bold text-white leading-tight"
            dangerouslySetInnerHTML={{ __html: sanitizeHtml(sectionHeadline) }}
          />
        </div>

        {/* Steps — compact vertical list, centered as a block but left-aligned internally */}
        <div className="flex flex-col gap-8 mb-12 mx-auto w-fit">
          {resolvedSteps.map((s, i) => (
            <div key={i} className="flex items-center gap-5">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-[var(--theme-accent)] text-xl font-bold text-white shadow-lg">
                {i + 1}
              </div>
              <h3 className="text-xl sm:text-2xl font-bold text-white">{s.title}</h3>
            </div>
          ))}
        </div>

        {/* CTA */}
        {showCtaButton && (
          <div className="text-center">
            <Button asChild size="lg" className="h-14 px-10 rounded-full bg-[var(--theme-button)] hover:bg-[var(--theme-button)] text-[var(--theme-button-text)] text-lg font-bold shadow-xl hover:opacity-90">
              <Link to={localePath(themeSlug ? `/${themeSlug}/config` : '/config')} className="flex items-center gap-2">
                <span>{ctaButtonText}{showPriceInButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}</span>
                {showPriceInButton && originalAmount && originalAmount > (paymentAmount ?? 0) && (
                  <span className="text-xs font-normal line-through opacity-70 decoration-white/50">{fmtCurrency(originalAmount)}</span>
                )}
              </Link>
            </Button>
          </div>
        )}
      </div>
    </section>
  )
}
