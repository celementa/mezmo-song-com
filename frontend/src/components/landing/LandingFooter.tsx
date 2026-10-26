import { Link } from 'react-router-dom'
import { localePath, useTr } from '@/lib/lang'

type Props = {
  logoUrl: string
  themeSlug: string | null
  tagline?: string
  companyName?: string
  email?: string
  phone?: string
  disclaimer?: string
  copyrightLine?: string
  /** centered: single-column centered layout (grace-melody style) */
  centered?: boolean
}

export function LandingFooter({
  tagline: taglineProp,
  email = 'support@example.ru',
  phone = '',
  disclaimer,
  copyrightLine: copyrightLineProp,
  centered = false,
}: Props) {
  const tr = useTr()
  const tagline = taglineProp ?? tr('Твоя музыка — Твоя история', 'Your music — your story')
  const copyrightLine = copyrightLineProp ?? tr('Все права защищены.', 'All rights reserved.')
  if (centered) {
    return (
      <footer className="border-t border-gray-100 bg-[var(--theme-accent-soft,#fafaf9)] py-12 text-sm text-gray-400">
        <div className="mx-auto max-w-2xl px-6 text-center space-y-4">
          {tagline && <p className="text-gray-500 text-sm">{tagline}</p>}
          <div className="flex items-center justify-center gap-5 text-xs flex-wrap">
            <Link to={localePath('/privacy')} className="hover:text-gray-600 transition-colors">{tr('Конфиденциальность', 'Privacy')}</Link>
            <Link to={localePath('/terms')} className="hover:text-gray-600 transition-colors">{tr('Условия', 'Terms')}</Link>
            <Link to={localePath('/contacts')} className="hover:text-gray-600 transition-colors">{tr('Контакты', 'Contact')}</Link>
          </div>
          {disclaimer && (
            <p className="text-[10px] text-gray-400 max-w-lg mx-auto leading-relaxed">
              <strong className="text-gray-500">{tr('Отказ от ответственности:', 'Disclaimer:')}</strong> {disclaimer}
            </p>
          )}
          <p className="text-[10px] text-gray-300">&copy; {new Date().getFullYear()} {copyrightLine}</p>
        </div>
      </footer>
    )
  }

  return (
    <footer className="border-t border-gray-100 bg-white py-12 text-sm text-gray-400">
      <div className="mx-auto max-w-7xl px-7 sm:px-9 md:px-11">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-8 sm:gap-12">
          <div className="flex flex-col items-center sm:items-start gap-3">
            <p className="text-gray-500 text-xs">{tagline}</p>
            <div className="text-xs space-y-1 text-gray-400">
              <a href={`mailto:${email}`} className="block hover:text-gray-600 transition-colors">{email}</a>
              {phone && <a href={`tel:${phone.replace(/\s/g, '')}`} className="block hover:text-gray-600 transition-colors">{phone}</a>}
            </div>
          </div>

          <div className="flex flex-col items-center sm:items-start gap-3">
            <h4 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{tr('Информация', 'Information')}</h4>
            <div className="flex flex-col gap-1.5">
              <Link to={localePath('/privacy')} className="text-xs hover:text-gray-600 transition-colors">{tr('Конфиденциальность', 'Privacy')}</Link>
              <Link to={localePath('/terms')} className="text-xs hover:text-gray-600 transition-colors">{tr('Условия', 'Terms')}</Link>
              <Link to={localePath('/contacts')} className="text-xs hover:text-gray-600 transition-colors">{tr('Контакты', 'Contact')}</Link>
            </div>
          </div>
        </div>

        <div className="mt-10 pt-6 border-t border-gray-100 text-center">
          {disclaimer && (
            <p className="max-w-2xl mx-auto text-[10px] sm:text-xs text-gray-400 leading-relaxed mb-4">
              <strong className="text-gray-500">{tr('Отказ от ответственности:', 'Disclaimer:')}</strong> {disclaimer}
            </p>
          )}
          <p className="text-[10px] text-gray-300">&copy; {new Date().getFullYear()} {copyrightLine}</p>
        </div>
      </div>
    </footer>
  )
}
