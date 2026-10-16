import { Instagram, Mail } from 'lucide-react'

interface Props {
  headline?: string | null
  message?: string | null
  email?: string | null
  instagramUrl?: string | null
  followText?: string | null
}

export function MaintenancePage({ headline, message, email, instagramUrl, followText }: Props) {
  const h = headline?.trim() || 'Скоро вернёмся'
  const m = message?.trim() || 'Сайт временно недоступен. Загляните позже или напишите на почту поддержки.'

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-b from-white to-gray-50 px-4 py-12">
      <div className="w-full max-w-md text-center space-y-6">
        <h1 className="text-3xl sm:text-4xl font-bold text-gray-900 tracking-tight">{h}</h1>
        <p className="text-base text-gray-600 whitespace-pre-line leading-relaxed">{m}</p>

        {(email || instagramUrl) && (
          <div className="pt-4 space-y-3">
            {email && (
              <a
                href={`mailto:${email}`}
                className="inline-flex items-center gap-2 text-sm text-gray-700 hover:text-gray-900"
              >
                <Mail className="h-4 w-4" />
                {email}
              </a>
            )}

            {instagramUrl && (
              <div className="pt-2">
                <p className="text-sm text-gray-500 mb-3">
                  {followText?.trim() || 'Следите за обновлениями'}
                </p>
                <a
                  href={instagramUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2 px-5 py-2.5 rounded-full bg-gradient-to-tr from-yellow-400 via-pink-500 to-purple-600 text-white text-sm font-medium hover:opacity-90 transition"
                >
                  <Instagram className="h-4 w-4" />
                  Instagram
                </a>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
