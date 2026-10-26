import { useLocation, useNavigate } from 'react-router-dom'
import { localePath, rememberLang, stripLang, useLang, type Lang } from '@/lib/lang'

const OPTIONS: { value: Lang; label: string; title: string }[] = [
  { value: 'ru', label: 'RU', title: 'Русский' },
  { value: 'en', label: 'EN', title: 'English' },
]

/** Переключатель языка «RU / EN» — пара к CurrencySwitch, та же таблетка. */
export function LangSwitch({ className = '' }: { className?: string }) {
  const lang = useLang()
  const { pathname, search, hash } = useLocation()
  const navigate = useNavigate()
  return (
    <div
      role="radiogroup"
      aria-label={lang === 'en' ? 'Language' : 'Язык'}
      className={`inline-flex rounded-full border border-[#d9cfbf] bg-white p-0.5 text-xs font-bold ${className}`}
    >
      {OPTIONS.map((o) => {
        const active = o.value === lang
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            title={o.title}
            lang={o.value}
            onClick={() => {
              if (active) return
              rememberLang(o.value)
              navigate(localePath(stripLang(pathname), o.value) + search + hash)
            }}
            className={`min-w-[2rem] rounded-full px-2.5 py-1 transition ${active ? 'bg-[#151210] text-white' : 'text-[#6b6558] hover:text-[#151210]'}`}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
