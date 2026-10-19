/**
 * Вопросы о песне: кому дарим, повод, стиль, история. В обоих вариантах это
 * одни и те же экраны — меняется только их место в последовательности.
 */

import { useState } from 'react'

import { EmotionGrid, GenreGrid, PickerSwitch } from '@/flow/steps/EmotionPicker'
import { CUSTOM_GENRE, resolveGenre } from '@/flow/types'
import { Chip, StepShell } from '../StepShell'
import { shellProps, type StepProps } from '../types'

const RELATIONSHIPS = ['Муж', 'Жена', 'Парень', 'Девушка', 'Мама', 'Папа', 'Бабушка', 'Дедушка', 'Друг', 'Другое']

const OCCASIONS = [
  { id: 'День рождения', icon: '🎂' },
  { id: 'Свадьба', icon: '💍' },
  { id: 'Годовщина', icon: '💛' },
  { id: 'Юбилей', icon: '🎉' },
  { id: 'Романтика', icon: '🌹' },
  { id: 'Просто так', icon: '✨' },
]

const STORY_PROMPTS = ['Как познакомились', 'Что цените', '«Наши» шутки', 'Мечты на будущее']

const inputClass =
  'w-full rounded-xl border border-[#E7DAC5] bg-[#FDFBF7] px-4 py-3 text-sm text-[#151210] outline-none transition placeholder:text-[#b4a893] focus:border-[#d9b978] focus:bg-white'

export function RecipientStep(p: StepProps) {
  const nameReady = p.form.recipientName.trim().length > 0
  return (
    <StepShell
      {...shellProps(p)}
      eyebrow="Кому дарим?"
      headline="Для кого эта песня?"
      primaryLabel="Далее"
      primaryDisabled={!nameReady}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', { relationship: p.form.relationship })
        p.onNext()
      }}
    >
      <div className="flex flex-wrap gap-2">
        {RELATIONSHIPS.map((r) => (
          <Chip
            key={r}
            label={r}
            selected={p.form.relationship === r}
            onClick={() => p.setForm({ relationship: r })}
          />
        ))}
      </div>
      <p className="mt-3 text-xs italic leading-relaxed text-[#9c9186]">
        «Другое» — например: учитель, коллега, крёстный, наставник
      </p>

      <label className="mt-5 block">
        <span className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">Как его/её зовут</span>
        <input
          className={`${inputClass} mt-2`}
          value={p.form.recipientName}
          onChange={(e) => p.setForm({ recipientName: e.target.value })}
          placeholder="напр. Алекс"
          autoComplete="off"
          maxLength={60}
        />
      </label>
    </StepShell>
  )
}

export function OccasionStep(p: StepProps) {
  return (
    <StepShell
      {...shellProps(p)}
      eyebrow="Повод"
      headline="По какому поводу песня?"
      primaryLabel="Далее"
      primaryDisabled={!p.form.occasion}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', { occasion: p.form.occasion })
        p.onNext()
      }}
    >
      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
        {OCCASIONS.map((o) => {
          const selected = p.form.occasion === o.id
          return (
            <button
              key={o.id}
              type="button"
              onClick={() => p.setForm({ occasion: o.id })}
              className={`flex flex-col items-center gap-2 rounded-2xl border px-3 py-4 text-center transition active:scale-[0.98] ${
                selected
                  ? 'border-[#d9b978] bg-[#FCF6EA] shadow-[0_10px_26px_-20px_rgba(185,133,46,0.9)]'
                  : 'border-[#EFE4D2] bg-white hover:border-[#e3d0ad]'
              }`}
            >
              <span className="text-2xl leading-none">{o.icon}</span>
              <span className="text-xs font-semibold leading-tight text-[#3b352f]">{o.id}</span>
            </button>
          )
        })}
      </div>
    </StepShell>
  )
}

/**
 * Шаг «эмоция». Жанр — доп. экран по ссылке под кнопками; выбранный жанр (или
 * свой стиль) остаётся уточнением к эмоции, а не заменяет её.
 */
export function EmotionStep(p: StepProps) {
  const [mode, setMode] = useState<'emotion' | 'genre'>('emotion')
  const genreReady = Boolean(resolveGenre(p.form.genre, p.form.customStyle))
  const ready = mode === 'emotion' ? Boolean(p.form.emotion) : genreReady

  return (
    <StepShell
      {...shellProps(p)}
      eyebrow={mode === 'emotion' ? 'Эмоция' : 'Жанр'}
      headline={mode === 'emotion' ? 'Какую эмоцию хотите передать?' : 'Выберите жанр'}
      primaryLabel="Далее"
      primaryDisabled={!ready}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', {
          emotion: p.form.emotion || undefined,
          genre: resolveGenre(p.form.genre, p.form.customStyle) || undefined,
        })
        p.onNext()
      }}
      footer={<PickerSwitch mode={mode} onSwitch={() => setMode(mode === 'emotion' ? 'genre' : 'emotion')} />}
    >
      {mode === 'emotion' ? (
        <EmotionGrid value={p.form.emotion} onChange={(emotion) => p.setForm({ emotion })} />
      ) : (
        <GenreGrid
          genre={p.form.genre}
          customStyle={p.form.customStyle}
          onChange={(patch) => p.setForm(patch.genre !== undefined && patch.genre !== CUSTOM_GENRE ? { ...patch, customStyle: '' } : patch)}
        />
      )}
    </StepShell>
  )
}

export function StoryStep(p: StepProps) {
  const appendPrompt = (prompt: string) => {
    const base = p.form.story.trim()
    p.setForm({ story: base ? `${base}\n${prompt}: ` : `${prompt}: ` })
  }

  return (
    <StepShell
      {...shellProps(p)}
      eyebrow="Расскажите историю"
      headline="О чём будет песня?"
      subhead="Хватит пары предложений — чем больше деталей, тем сильнее эмоции."
      primaryLabel="Далее"
      primaryDisabled={p.form.story.trim().length < 10}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', { storyLength: p.form.story.trim().length })
        p.onNext()
      }}
    >
      <div className="flex flex-wrap gap-2">
        {STORY_PROMPTS.map((prompt) => (
          <button
            key={prompt}
            type="button"
            onClick={() => appendPrompt(prompt)}
            className="rounded-full border border-dashed border-[#E0CFAC] bg-[#FDFBF7] px-3.5 py-1.5 text-xs font-semibold text-[#8a6621] transition hover:bg-[#FCF6EA]"
          >
            {prompt}
          </button>
        ))}
      </div>

      <textarea
        className={`${inputClass} mt-3 min-h-[120px] resize-y`}
        value={p.form.story}
        onChange={(e) => p.setForm({ story: e.target.value })}
        placeholder="Начните вводить текст здесь…"
        maxLength={2000}
      />

      <div className="mt-3 rounded-2xl border border-dashed border-[#E0CFAC] bg-[#FDFBF7] p-3.5">
        <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">💡 Пример — годовщина</div>
        <p className="mt-1.5 text-xs italic leading-relaxed text-[#6b6259]">
          «Каждое утро он приносит мне кофе в постель. Мы зовём его „капитан завтрак“ — воскресные блинчики его коронное
          блюдо.»
        </p>
      </div>
    </StepShell>
  )
}
