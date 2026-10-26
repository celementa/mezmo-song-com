import { useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTr } from '@/lib/lang'

function sanitizeHtml(html: string): string {
  return html.replace(/<\/?(?!strong|em|br\s*\/?)(?!span\b)([a-z][a-z0-9]*)\b[^>]*>/gi, '')
}

type FaqItem = { q: string; a: string }

type Props = {
  items: FaqItem[]
  sectionHeadline?: string
}

export function FaqSection({
  items,
  sectionHeadline: sectionHeadlineProp,
}: Props) {
  const tr = useTr()
  const sectionHeadline = sectionHeadlineProp ?? tr(
    'Частые <span class="text-[var(--theme-accent-text)] italic">вопросы</span>',
    'Frequently asked <span class="text-[var(--theme-accent-text)] italic">questions</span>',
  )
  const [openIndex, setOpenIndex] = useState<number | null>(null)

  return (
    <section aria-labelledby="faq-title" className="space-y-10 max-w-3xl mx-auto pb-8">
      <h2 id="faq-title" className="text-center font-serif text-3xl font-bold text-gray-900" dangerouslySetInnerHTML={{ __html: sanitizeHtml(sectionHeadline) }} />
      <div className="space-y-3">
        {items.map((faq, i) => {
          const isOpen = openIndex === i
          return (
            <div key={i} className="rounded-2xl border border-gray-100 bg-white overflow-hidden transition-shadow hover:shadow-md">
              <button
                type="button"
                onClick={() => setOpenIndex(isOpen ? null : i)}
                className="flex w-full items-center justify-between gap-4 p-5 text-left"
              >
                <h3 className="font-bold text-gray-900 text-lg">{faq.q}</h3>
                <ChevronDown className={cn(
                  'h-5 w-5 shrink-0 text-gray-400 transition-transform duration-200',
                  isOpen && 'rotate-180 text-[var(--theme-accent-text)]'
                )} />
              </button>
              <div className={cn(
                'grid transition-all duration-200',
                isOpen ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0'
              )}>
                <div className="overflow-hidden">
                  <p className="px-5 pb-5 text-gray-600 leading-relaxed">{faq.a}</p>
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
