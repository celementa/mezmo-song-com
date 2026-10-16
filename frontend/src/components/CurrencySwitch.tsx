import { setCurrency, useCurrency, type Currency } from '@/lib/currency'

const OPTIONS: { value: Currency; label: string; title: string }[] = [
  { value: 'USD', label: '$', title: 'Цены в долларах США' },
  { value: 'RUB', label: '₽', title: 'Цены в российских рублях' },
]

/** Переключатель валюты «$ / ₽» — как на международных сайтах. */
export function CurrencySwitch({ className = '' }: { className?: string }) {
  const currency = useCurrency()
  return (
    <div
      role="radiogroup"
      aria-label="Валюта"
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
            title={o.title}
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
