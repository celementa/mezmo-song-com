import { setCurrency, useCurrency, type Currency } from '@/lib/currency'
import { useTr } from '@/lib/lang'

const OPTIONS: { value: Currency; label: string; title: { ru: string; en: string } }[] = [
  { value: 'USD', label: '$', title: { ru: 'Цены в долларах США', en: 'Prices in US dollars' } },
  { value: 'RUB', label: '₽', title: { ru: 'Цены в российских рублях', en: 'Prices in Russian rubles' } },
]

/** Переключатель валюты «$ / ₽» — как на международных сайтах. */
export function CurrencySwitch({ className = '' }: { className?: string }) {
  const currency = useCurrency()
  const tr = useTr()
  return (
    <div
      role="radiogroup"
      aria-label={tr('Валюта', 'Currency')}
      className={`inline-flex rounded-full border border-[#d9cfbf] bg-white p-0.5 text-xs font-bold ${className}`}
    >
      {OPTIONS.map((o) => {
        const active = o.value === currency
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={active}
            title={tr(o.title.ru, o.title.en)}
            onClick={() => setCurrency(o.value)}
            className={`min-w-[2rem] rounded-full px-2.5 py-1 transition ${active ? 'bg-[#151210] text-white' : 'text-[#6b6558] hover:text-[#151210]'}`}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
