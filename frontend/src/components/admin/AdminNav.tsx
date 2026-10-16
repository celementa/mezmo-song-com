// Переключатель между страницами админки. Текущая страница подсвечена и не
// является ссылкой, чтобы клик по ней не перезагружал то же самое.
//
// Страницы доступны только роли admin. Роль ads (блогеры, модераторы рекламы)
// сюда не попадает — ей открыт только /admin/ads, поэтому ссылки на заказы,
// аналитику и настройки для неё бессмысленны.

const PAGES = [
  { path: '/admin/orders', label: 'Заказы' },
  { path: '/admin/analytics', label: 'Аналитика' },
  { path: '/admin/settings', label: 'Настройки' },
  { path: '/admin/emails', label: 'Письма' },
  { path: '/admin/lesroses', label: 'Les Roses' },
] as const

export function AdminNav({ current }: { current: (typeof PAGES)[number]['path'] }) {
  return (
    <div className="flex overflow-hidden rounded-lg border border-gray-300 bg-white">
      {PAGES.map((p) =>
        p.path === current ? (
          <span key={p.path} className="bg-gray-900 px-3 py-2 text-sm font-medium text-white">
            {p.label}
          </span>
        ) : (
          <a
            key={p.path}
            href={p.path}
            className="px-3 py-2 text-sm font-medium text-gray-600 hover:bg-gray-100"
          >
            {p.label}
          </a>
        ),
      )}
    </div>
  )
}
