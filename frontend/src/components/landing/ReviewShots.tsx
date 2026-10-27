// Скриншоты отзывов для gold-лендинга. Скриншоты разной формы (полоска
// Telegram 3,9:1 и почти квадратное письмо), поэтому вместо одинаковых слотов
// они раскладываются по трём колонкам «кирпичами» по реальным пропорциям.

export interface ReviewShot {
  src: string
  alt: string
  // ширина / высота исходного файла
  ratio: number
}

const cardShadow = { boxShadow: '0 10px 30px -12px rgba(21,18,16,.28), 0 2px 6px rgba(21,18,16,.06)' }

function Shot({ shot }: { shot: ReviewShot }) {
  return (
    <figure className="bg-white rounded-2xl overflow-hidden" style={cardShadow}>
      <img src={shot.src} alt={shot.alt} loading="lazy" className="block w-full h-auto" style={{ aspectRatio: String(shot.ratio) }} />
    </figure>
  )
}

// Жадная раскладка: самый высокий скриншот — в самую короткую колонку.
// Каждый скрин получается ширины телефона, ни один не растягивается на всю сетку.
export function ReviewShots({ shots }: { shots: ReviewShot[] }) {
  const cols: { items: ReviewShot[]; h: number }[] = [{ items: [], h: 0 }, { items: [], h: 0 }, { items: [], h: 0 }]
  ;[...shots].sort((x, y) => 1 / y.ratio - 1 / x.ratio).forEach((s) => {
    const c = cols.reduce((m, c) => (c.h < m.h ? c : m))
    c.items.push(s)
    c.h += 1 / s.ratio
  })
  return (
    <>
      <div className="hidden md:grid grid-cols-3 gap-5 items-start">
        {cols.map((c, i) => (
          <div key={i} className="flex flex-col gap-5">
            {c.items.map((s) => <Shot key={s.src} shot={s} />)}
          </div>
        ))}
      </div>
      <div className="md:hidden flex flex-col gap-4 max-w-sm mx-auto">
        {shots.map((s) => <Shot key={s.src} shot={s} />)}
      </div>
    </>
  )
}
