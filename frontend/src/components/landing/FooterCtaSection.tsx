import { Link } from 'react-router-dom'
import { Button } from '@/components/ui/button'

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)(?!span\b)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

type Props = {
  headline: string
  subtitle: string
  fmtCurrency: (v: number | null | undefined) => string
  paymentAmount: number | null
  originalAmount: number | null
  showPriceInButton?: boolean
  themeSlug: string | null
  securityBadge?: string
  quotaLine?: string
  ctaButtonText?: string
  showCtaButton?: boolean
}

export function FooterCtaSection({
  headline,
  subtitle,
  fmtCurrency,
  paymentAmount,
  originalAmount,
  showPriceInButton = true,
  themeSlug,
  securityBadge = '',
  quotaLine = 'Осталось бесплатных слотов: 11',
  ctaButtonText = 'Создать свою песню',
  showCtaButton = true,
}: Props) {
  return (
    <section className="text-center space-y-6 pb-12">
      <h2 className="font-serif text-3xl sm:text-4xl font-bold text-gray-900">{headline}</h2>
      <p className="text-gray-600 text-base sm:text-lg" dangerouslySetInnerHTML={{ __html: sanitizeHtml(subtitle) }} />

      {showCtaButton && (
        <div className="pt-4">
          <Button asChild size="lg" className="h-14 px-10 rounded-full bg-[var(--theme-button)] hover:bg-[var(--theme-button)] text-[var(--theme-button-text)] text-lg font-bold shadow-2xl shadow-[var(--theme-accent-soft)] hover:opacity-90 transition-all">
            <Link to={themeSlug ? `/${themeSlug}/config` : '/config'} className="flex items-center gap-2">
              <span>{ctaButtonText}{showPriceInButton ? ` — ${fmtCurrency(paymentAmount)}` : ''}</span>
              {showPriceInButton && <span className="text-xs sm:text-sm font-normal line-through opacity-80 text-[var(--theme-accent-soft)]">{fmtCurrency(originalAmount)}</span>}
            </Link>
          </Button>
        </div>
      )}

      <div className="flex flex-wrap justify-center gap-x-6 gap-y-2 text-[11px] sm:text-xs font-bold text-gray-400 uppercase tracking-wide pt-4">
        {securityBadge && <span>{securityBadge}</span>}
        {quotaLine && <span>{quotaLine}</span>}
      </div>
    </section>
  )
}
