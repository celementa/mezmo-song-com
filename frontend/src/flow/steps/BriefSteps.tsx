/**
 * Шаги брифа варианта 1 в дизайне движка /create: каждый шаг — карточка
 * StepShell (прогресс «ШАГ N ИЗ M», капс-заголовок золотом, серифный h1,
 * навигация внутри карточки). Компоненты и палитра переиспользуются из
 * routes/create/StepShell — единый дизайн всех новых флоу по построению,
 * а не копией классов.
 */

import { useState } from 'react'

import { Chip, StepShell } from '@/routes/create/StepShell'
import { genderFromRelationship } from '@/lib/recipientGender'
import { useLang, useTr } from '@/lib/lang'
import {
  CUSTOM_GENRE,
  OCCASIONS,
  RELATIONSHIPS,
  STORY_PROMPTS,
  STORY_PROMPTS_EN,
  answerText,
  resolveGenre,
  type FlowAnswers,
} from '../types'
import { EmotionGrid, GenreGrid, PickerSwitch } from './EmotionPicker'

/** Пропсы оболочки, которые FlowRoute прокидывает каждому шагу. */
export type ShellPassthrough = {
  stepIndex: number
  stepTotal: number
  onBack: () => void
  canGoBack: boolean
  busy?: boolean
  error?: string | null
}

type StepProps = {
  answers: FlowAnswers
  patch: (fields: Partial<FlowAnswers>) => void
  shell: ShellPassthrough
  onNext: () => void
  primaryLabel: string
}

const inputClass =
  'w-full rounded-xl border border-[#E7DAC5] bg-[#FDFBF7] px-4 py-3 text-sm text-[#151210] outline-none transition placeholder:text-[#b4a893] focus:border-[#d9b978] focus:bg-white'

const labelClass = 'text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]'

/** Шаг 1 — кому дарим и как зовут. */
export function RecipientStep({ answers, patch, shell, onNext, primaryLabel }: StepProps) {
  const tr = useTr()
  const lang = useLang()
  // Пол выводится из отношения, но «Друг» и «Другое» его не задают: «Друг» в
  // этой форме выбирают и для подруги, а по имени пол не угадывается — Саша,
  // Женя, Никита в уменьшительной форме кончаются на -а/-я и при этом мужские.
  const derived = genderFromRelationship(answers.relationship)
  const needsGenderToggle = Boolean(answers.relationship) && !derived

  return (
    <StepShell
      {...shell}
      eyebrow={tr('Кому дарим?', 'Who is it for?')}
      headline={tr('Для кого эта песня?', 'Who is this song for?')}
      primaryLabel={primaryLabel}
      onPrimary={onNext}
    >
      <div className="flex flex-wrap gap-2">
        {RELATIONSHIPS.map((value) => (
          <Chip
            key={value}
            label={answerText(value, lang)}
            selected={answers.relationship === value}
            onClick={() => patch({ relationship: value, recipientGender: genderFromRelationship(value) ?? '' })}
          />
        ))}
      </div>
      {answers.relationship === 'Другое' && (
        <p className="mt-3 text-xs italic leading-relaxed text-[#9c9186]">
          {tr(
            '«Другое» — например: учитель, коллега, крёстный, наставник',
            '"Other" — for example: a teacher, colleague, godfather, mentor',
          )}
        </p>
      )}

      {needsGenderToggle && (
        <div className="mt-5">
          <span className={labelClass}>{tr('Кому именно', 'Which one')}</span>
          <div className="mt-2 flex gap-2">
            {[
              { value: 'male', label: tr('Ему', 'Him') },
              { value: 'female', label: tr('Ей', 'Her') },
            ].map((option) => (
              <Chip
                key={option.value}
                label={option.label}
                selected={answers.recipientGender === option.value}
                onClick={() => patch({ recipientGender: option.value })}
              />
            ))}
          </div>
        </div>
      )}

      <label className="mt-5 block">
        <span className={labelClass}>{tr('Как его/её зовут', 'Their name')}</span>
        <input
          className={`${inputClass} mt-2`}
          value={answers.recipientName}
          onChange={(e) => patch({ recipientName: e.target.value })}
          placeholder={tr('напр. Алекс', 'e.g. Alex')}
          autoComplete="off"
          maxLength={60}
        />
      </label>
    </StepShell>
  )
}

/** Шаг 2 — повод. */
export function OccasionStep({ answers, patch, shell, onNext, primaryLabel }: StepProps) {
  const tr = useTr()
  return (
    <StepShell
      {...shell}
      eyebrow={tr('Повод', 'Occasion')}
      headline={tr('По какому поводу песня?', 'What is the occasion?')}
      primaryLabel={primaryLabel}
      onPrimary={onNext}
    >
      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
        {OCCASIONS.map((occasion) => {
          const selected = answers.occasion === occasion.value
          return (
            <button
              key={occasion.value}
              type="button"
              onClick={() => patch({ occasion: occasion.value })}
              className={`flex flex-col items-center gap-2 rounded-2xl border px-3 py-4 text-center transition active:scale-[0.98] ${
                selected
                  ? 'border-[#d9b978] bg-[#FCF6EA] shadow-[0_10px_26px_-20px_rgba(185,133,46,0.9)]'
                  : 'border-[#EFE4D2] bg-white hover:border-[#e3d0ad]'
              }`}
            >
              <span className="text-2xl leading-none">{occasion.icon}</span>
              <span className="text-xs font-semibold leading-tight text-[#3b352f]">{tr(occasion.value, occasion.en)}</span>
            </button>
          )
        })}
      </div>
    </StepShell>
  )
}

/**
 * Шаг 3 — email. Здесь же на сервере создаётся заказ: в варианте 1 адрес
 * собирается до генерации именно для того, чтобы у брошенной корзины он остался.
 */
export function EmailStep({ answers, patch, shell, onNext, primaryLabel }: StepProps) {
  const tr = useTr()
  const optionalMark = <span className="ml-1 font-semibold normal-case tracking-normal text-[#c19a4f]">{tr('необязательно', 'optional')}</span>
  return (
    <StepShell
      {...shell}
      eyebrow="Email"
      headline={tr('Куда отправить песню?', 'Where should we send the song?')}
      primaryLabel={primaryLabel}
      onPrimary={onNext}
    >
      <label className="block">
        <span className={labelClass}>Email</span>
        <input
          className={`${inputClass} mt-2`}
          type="email"
          inputMode="email"
          value={answers.email}
          onChange={(e) => patch({ email: e.target.value })}
          placeholder={tr('you@mail.ru', 'you@email.com')}
          autoComplete="email"
        />
      </label>
      <p className="mt-2 text-xs font-medium text-[#8a6621]">
        {tr('Не забудьте проверить папку со спамом!', "Don't forget to check your spam folder!")}
      </p>
      <label className="mt-4 block">
        <span className={labelClass}>
          {tr('Телефон', 'Phone')} {optionalMark}
        </span>
        <input
          className={`${inputClass} mt-2`}
          type="tel"
          inputMode="tel"
          value={answers.phone}
          onChange={(e) => patch({ phone: e.target.value })}
          placeholder={tr('+7 900 123-45-67', '+1 555 123-4567')}
          autoComplete="tel"
        />
      </label>
      <p className="mt-3 text-xs leading-relaxed text-[#9c9186]">
        {tr('Пришлём песню на этот email.', "We'll send the song to this email.")}
      </p>
    </StepShell>
  )
}

/**
 * Шаг 4 — эмоция (жанр — доп. экран по ссылке, см. EmotionPicker). Выбора
 * голоса нет: пол вокала сервер выводит из адресата.
 */
export function EmotionStep({ answers, patch, shell, onNext, primaryLabel }: StepProps) {
  const tr = useTr()
  const [mode, setMode] = useState<'emotion' | 'genre'>('emotion')
  return (
    <StepShell
      {...shell}
      eyebrow={mode === 'emotion' ? tr('Эмоция', 'Emotion') : tr('Жанр', 'Genre')}
      headline={
        mode === 'emotion'
          ? tr('Какую эмоцию хотите передать?', 'Which feeling do you want to convey?')
          : tr('Выберите жанр', 'Choose a genre')
      }
      primaryLabel={primaryLabel}
      onPrimary={onNext}
      footer={<PickerSwitch mode={mode} onSwitch={() => setMode(mode === 'emotion' ? 'genre' : 'emotion')} />}
    >
      {mode === 'emotion' ? (
        <EmotionGrid value={answers.emotion} onChange={(emotion) => patch({ emotion })} />
      ) : (
        <GenreGrid
          genre={answers.genre}
          customStyle={answers.customStyle}
          onChange={(fields) => patch(fields.genre !== undefined && fields.genre !== CUSTOM_GENRE ? { ...fields, customStyle: '' } : fields)}
        />
      )}
    </StepShell>
  )
}

/** Жанр из ответов, если выбран; иначе пусто — модель подберёт под эмоцию. */
export function answersGenre(answers: FlowAnswers): string {
  return resolveGenre(answers.genre, answers.customStyle)
}

/** Шаг 5 — история. */
export function StoryStep({ answers, patch, shell, onNext, primaryLabel }: StepProps) {
  const tr = useTr()
  const appendPrompt = (prompt: (typeof STORY_PROMPTS)[number]) => {
    const separator = answers.story.trim() ? '\n' : ''
    patch({ story: `${answers.story}${separator}${tr(prompt, STORY_PROMPTS_EN[prompt])}: ` })
  }

  return (
    <StepShell
      {...shell}
      eyebrow={tr('Расскажите историю', 'Tell us your story')}
      headline={tr('О чём будет песня?', "What's the song about?")}
      subhead={tr(
        'Из этого сделаем текст песни — хватит пары предложений.',
        'We will write the lyrics from this. A couple of sentences is enough.',
      )}
      primaryLabel={primaryLabel}
      onPrimary={onNext}
    >
      <div className="flex flex-wrap gap-2">
        {STORY_PROMPTS.map((prompt) => (
          <button
            key={prompt}
            type="button"
            onClick={() => appendPrompt(prompt)}
            className="rounded-full border border-dashed border-[#E0CFAC] bg-[#FDFBF7] px-3.5 py-1.5 text-xs font-semibold text-[#8a6621] transition hover:bg-[#FCF6EA]"
          >
            {tr(prompt, STORY_PROMPTS_EN[prompt])}
          </button>
        ))}
      </div>

      <textarea
        className={`${inputClass} mt-3 min-h-[120px] resize-y`}
        value={answers.story}
        onChange={(e) => patch({ story: e.target.value })}
        placeholder={tr('Начните вводить текст здесь…', 'Start typing here…')}
        maxLength={2000}
      />

      <div className="mt-3 rounded-2xl border border-dashed border-[#E0CFAC] bg-[#FDFBF7] p-3.5">
        <div className={labelClass}>💡 {tr('Пример — годовщина', 'Example — anniversary')}</div>
        <p className="mt-1.5 text-xs italic leading-relaxed text-[#6b6259]">
          {tr(
            '«Каждое утро он приносит мне кофе в постель. Мы зовём его „капитан завтрак“ — воскресные блинчики его коронное блюдо.»',
            '"Every morning he brings me coffee in bed. We call him "Captain Breakfast" — Sunday pancakes are his signature dish."',
          )}
        </p>
      </div>
    </StepShell>
  )
}
