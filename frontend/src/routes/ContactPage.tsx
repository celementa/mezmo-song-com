import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft, Mail, Clock } from 'lucide-react'

import { cachedSettingsGet } from '@/lib/settingsCache'

type FooterSettings = {
  email?: string
  companyName?: string
}

const DEFAULT_EMAIL = 'support@example.ru'

export function ContactPage() {
  const [footer, setFooter] = useState<FooterSettings>({})

  useEffect(() => {
    let cancelled = false
    cachedSettingsGet<{ publicSiteConfig?: { footer?: FooterSettings } | null }>('/api/public/settings')
      .then((res) => {
        if (cancelled) return
        const f = res?.publicSiteConfig?.footer
        if (f && typeof f === 'object') setFooter(f)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  const email = (footer.email && footer.email.trim()) || DEFAULT_EMAIL
  const siteHost = typeof window !== 'undefined' ? window.location.host : ''
  const siteUrl = typeof window !== 'undefined' ? window.location.origin : ''

  return (
    <div className="min-h-screen bg-white">
      <div className="mx-auto max-w-3xl px-4 py-12 sm:py-16">
        <Link to="/" className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-gray-600 transition-colors mb-8">
          <ArrowLeft className="h-4 w-4" />
          На главную
        </Link>

        <h1 className="text-3xl font-bold text-gray-900 mb-2">Контакты</h1>
        <p className="text-gray-500 mb-10">Вопросы по заказу и технической поддержке.</p>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 mb-12">
          <a
            href={`mailto:${email}`}
            className="group flex flex-col items-center gap-3 rounded-xl border border-gray-200 p-6 text-center transition-all hover:border-rose-200 hover:shadow-md"
          >
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-rose-50 text-rose-500 transition-colors group-hover:bg-rose-100">
              <Mail className="h-5 w-5" />
            </div>
            <h3 className="font-semibold text-gray-800">Электронная почта</h3>
            <p className="text-sm text-gray-500">Напишите нам — ответим в рабочее время</p>
            <span className="text-sm font-medium text-rose-500">{email}</span>
          </a>

          <div className="flex flex-col items-center gap-3 rounded-xl border border-gray-200 p-6 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-blue-50 text-blue-500">
              <Clock className="h-5 w-5" />
            </div>
            <h3 className="font-semibold text-gray-800">Время ответа</h3>
            <p className="text-sm text-gray-500">Обычно в течение 1 рабочего дня</p>
          </div>
        </div>

        <div className="space-y-8 text-gray-600 text-[15px] leading-relaxed">
          <section className="rounded-xl border border-gray-200 p-6">
            <h2 className="text-lg font-semibold text-gray-800 mb-4">Часы поддержки (ориентир)</h2>
            <div className="space-y-2 text-sm">
              <div className="flex justify-between py-2 border-b border-gray-100">
                <span className="text-gray-500">Пн–Пт</span>
                <span className="font-medium text-gray-700">10:00 – 19:00 МСК</span>
              </div>
              <div className="flex justify-between py-2">
                <span className="text-gray-500">Сб–Вс</span>
                <span className="font-medium text-gray-700">Выходной или сокращённо</span>
              </div>
            </div>
          </section>

          <section>
            <h2 className="text-lg font-semibold text-gray-800 mb-4">Частые вопросы</h2>
            <div className="space-y-4">
              <div className="rounded-lg bg-gray-50 p-4">
                <h3 className="font-medium text-gray-800 mb-1">Когда будет готова песня?</h3>
                <p className="text-sm text-gray-500">Срок зависит от очереди; статус приходит на электронную почту и отображается на странице заказа.</p>
              </div>
              <div className="rounded-lg bg-gray-50 p-4">
                <h3 className="font-medium text-gray-800 mb-1">Как получить файл?</h3>
                <p className="text-sm text-gray-500">По ссылке из письма или на странице заказа после статуса «Готово».</p>
              </div>
              <div className="rounded-lg bg-gray-50 p-4">
                <h3 className="font-medium text-gray-800 mb-1">Способы оплаты</h3>
                <p className="text-sm text-gray-500">Банковские карты и другие методы ЮKassa / выбранного провайдера — см. на шаге оплаты.</p>
              </div>
            </div>
          </section>

          <section className="text-center py-6">
            <div className="p-6 rounded-xl bg-gray-50">
              <p className="font-semibold text-gray-800 mb-1">Сервис персональных музыкальных композиций</p>
              {siteHost && (
                <a href={siteUrl} className="text-sm text-rose-500 hover:underline mt-2 inline-block">
                  {siteHost}
                </a>
              )}
            </div>
          </section>
        </div>
      </div>
    </div>
  )
}
