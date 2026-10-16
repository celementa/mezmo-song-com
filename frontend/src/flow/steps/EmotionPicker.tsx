/**
 * Выбор эмоции с фолбэком на жанр — один компонент на обе воронки (/config и
 * /create), чтобы карточки, подписи и логика «свой стиль» не разъезжались.
 *
 * Основной экран — шесть карточек эмоций, все видны без скролла. Ссылка
 * «Выбрать жанр вместо этого» открывает доп. экран с шестью жанрами; плитка
 * «Свой стиль» раскрывает поле свободного описания. Экран жанра — состояние
 * внутри шага, а не отдельный шаг воронки: в счётчике «шаг N из M» его нет.
 */

import { CUSTOM_GENRE, EMOTIONS, GENRES } from '../types'

const inputClass =
  'w-full rounded-xl border border-[#E7DAC5] bg-[#FDFBF7] px-4 py-3 text-sm text-[#151210] outline-none transition placeholder:text-[#b4a893] focus:border-[#d9b978] focus:bg-white'

const tileClass = (selected: boolean) =>
  `flex flex-col items-center gap-2 rounded-2xl border px-2 py-4 text-center transition active:scale-[0.98] ${
    selected
      ? 'border-[#d9b978] bg-[#FCF6EA] shadow-[0_10px_26px_-20px_rgba(185,133,46,0.9)]'
      : 'border-[#EFE4D2] bg-white hover:border-[#e3d0ad]'
  }`

export function EmotionGrid({ value, onChange }: { value: string; onChange: (emotion: string) => void }) {
  return (
    <>
      <div className="grid grid-cols-3 gap-2.5">
        {EMOTIONS.map((e) => (
          <button key={e.value} type="button" onClick={() => onChange(e.value)} className={tileClass(value === e.value)}>
            <span className="text-2xl leading-none">{e.icon}</span>
            <span lang="ru" className="max-w-full break-words text-[11px] font-semibold leading-tight text-[#3b352f] [hyphens:auto]">{e.value}</span>
          </button>
        ))}
      </div>
      <p className="mt-3 text-xs italic leading-relaxed text-[#9c9186]">
        «Другое» — двумя словами опишете на следующем шаге, в истории песни
      </p>
    </>
  )
}

export function GenreGrid({
  genre,
  customStyle,
  onChange,
}: {
  genre: string
  customStyle: string
  onChange: (patch: { genre?: string; customStyle?: string }) => void
}) {
  return (
    <>
      <div className="grid grid-cols-3 gap-2.5">
        {GENRES.map((g) => (
          <button key={g.id} type="button" onClick={() => onChange({ genre: g.id })} className={tileClass(genre === g.id)}>
            <span className="text-2xl leading-none">{g.icon}</span>
            <span className="text-xs font-semibold leading-tight text-[#3b352f]">{g.label}</span>
          </button>
        ))}
      </div>
      {genre === CUSTOM_GENRE ? (
        <label className="mt-4 block border-t border-dashed border-[#E0CFAC] pt-4">
          <span className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">Опишите свой стиль</span>
          <input
            className={`${inputClass} mt-2`}
            value={customStyle}
            onChange={(e) => onChange({ customStyle: e.target.value })}
            placeholder="напр. инди-фолк с гитарой"
            maxLength={80}
            autoFocus
          />
        </label>
      ) : null}
    </>
  )
}

/** Ссылка-фолбэк под кнопками: переключает эмоцию ↔ жанр. */
export function PickerSwitch({ mode, onSwitch }: { mode: 'emotion' | 'genre'; onSwitch: () => void }) {
  return (
    <button
      type="button"
      onClick={onSwitch}
      className="mt-4 block w-full text-center text-sm font-medium text-[#8a6621] underline underline-offset-4"
    >
      {mode === 'emotion' ? 'Выбрать жанр вместо этого →' : '← Вернуться к эмоции'}
    </button>
  )
}
