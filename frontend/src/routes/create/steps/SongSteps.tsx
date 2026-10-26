/**
 * Вопросы о песне: кому дарим, повод, стиль, история. В обоих вариантах это
 * одни и те же экраны — меняется только их место в последовательности.
 */

import { useState } from 'react'

import { EmotionGrid, GenreGrid, PickerSwitch } from '@/flow/steps/EmotionPicker'
import { CUSTOM_GENRE, OCCASIONS, RELATIONSHIPS, RELATIONSHIP_EN, STORY_PROMPTS, STORY_PROMPTS_EN, resolveGenre } from '@/flow/types'
import { useTr } from '@/lib/lang'
import { Chip, StepShell } from '../StepShell'
import { shellProps, type StepProps } from '../types'


const inputClass =
  'w-full rounded-xl border border-[#E7DAC5] bg-[#FDFBF7] px-4 py-3 text-sm text-[#151210] outline-none transition placeholder:text-[#b4a893] focus:border-[#d9b978] focus:bg-white'

export function RecipientStep(p: StepProps) {
  const tr = useTr()
  const nameReady = p.form.recipientName.trim().length > 0
  return (
    <StepShell
      {...shellProps(p)}
      eyebrow={tr('Кому дарим?', 'Who is it for?')}
      headline={tr('Для кого эта песня?', 'Who is this song for?')}
      primaryLabel={tr('Далее', 'Next')}
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
            label={tr(r, RELATIONSHIP_EN[r])}
            selected={p.form.relationship === r}
            onClick={() => p.setForm({ relationship: r })}
          />
        ))}
      </div>
      <p className="mt-3 text-xs italic leading-relaxed text-[#9c9186]">
        {tr(
          '«Другое» — например: учитель, коллега, крёстный, наставник',
          '"Other" — for example: teacher, colleague, godparent, mentor',
        )}
      </p>

      <label className="mt-5 block">
        <span className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">
          {tr('Как его/её зовут', 'Their name')}
        </span>
        <input
          className={`${inputClass} mt-2`}
          value={p.form.recipientName}
          onChange={(e) => p.setForm({ recipientName: e.target.value })}
          placeholder={tr('напр. Алекс', 'e.g. Alex')}
          autoComplete="off"
          maxLength={60}
        />
      </label>
    </StepShell>
  )
}

export function OccasionStep(p: StepProps) {
  const tr = useTr()
  return (
    <StepShell
      {...shellProps(p)}
      eyebrow={tr('Повод', 'Occasion')}
      headline={tr('По какому поводу песня?', 'What is the occasion?')}
      primaryLabel={tr('Далее', 'Next')}
      primaryDisabled={!p.form.occasion}
      onPrimary={() => {
        p.track(p.stepId, p.stepIndex, 'next', { occasion: p.form.occasion })
        p.onNext()
      }}
    >
      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
        {OCCASIONS.map((o) => {
          const selected = p.form.occasion === o.value
          return (
            <button
              key={o.value}
              type="button"
              onClick={() => p.setForm({ occasion: o.value })}
              className={`flex flex-col items-center gap-2 rounded-2xl border px-3 py-4 text-center transition active:scale-[0.98] ${
                selected
                  ? 'border-[#d9b978] bg-[#FCF6EA] shadow-[0_10px_26px_-20px_rgba(185,133,46,0.9)]'
                  : 'border-[#EFE4D2] bg-white hover:border-[#e3d0ad]'
              }`}
            >
              <span className="text-2xl leading-none">{o.icon}</span>
              <span className="text-xs font-semibold leading-tight text-[#3b352f]">{tr(o.value, o.en)}</span>
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
  const tr = useTr()
  const [mode, setMode] = useState<'emotion' | 'genre'>('emotion')
  const genreReady = Boolean(resolveGenre(p.form.genre, p.form.customStyle))
  const ready = mode === 'emotion' ? Boolean(p.form.emotion) : genreReady

  return (
    <StepShell
      {...shellProps(p)}
      eyebrow={mode === 'emotion' ? tr('Эмоция', 'Emotion') : tr('Жанр', 'Genre')}
      headline={
        mode === 'emotion'
          ? tr('Какую эмоцию хотите передать?', 'What feeling do you want to convey?')
          : tr('Выберите жанр', 'Choose a genre')
      }
      primaryLabel={tr('Далее', 'Next')}
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
  const tr = useTr()
  const appendPrompt = (prompt: string) => {
    const base = p.form.story.trim()
    p.setForm({ story: base ? `${base}\n${prompt}: ` : `${prompt}: ` })
  }

  return (
    <StepShell
      {...shellProps(p)}
      eyebrow={tr('Расскажите историю', 'Tell your story')}
      headline={tr('О чём будет песня?', 'What is the song about?')}
      subhead={tr(
        'Хватит пары предложений — чем больше деталей, тем сильнее эмоции.',
        'A few sentences is enough. The more details, the more heartfelt the song.',
      )}
      primaryLabel={tr('Далее', 'Next')}
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
            onClick={() => appendPrompt(tr(prompt, STORY_PROMPTS_EN[prompt]))}
            className="rounded-full border border-dashed border-[#E0CFAC] bg-[#FDFBF7] px-3.5 py-1.5 text-xs font-semibold text-[#8a6621] transition hover:bg-[#FCF6EA]"
          >
            {tr(prompt, STORY_PROMPTS_EN[prompt])}
          </button>
        ))}
      </div>

      <textarea
        className={`${inputClass} mt-3 min-h-[120px] resize-y`}
        value={p.form.story}
        onChange={(e) => p.setForm({ story: e.target.value })}
        placeholder={tr('Начните вводить текст здесь…', 'Start typing here…')}
        maxLength={2000}
      />

      <div className="mt-3 rounded-2xl border border-dashed border-[#E0CFAC] bg-[#FDFBF7] p-3.5">
        <div className="text-[11px] font-bold uppercase tracking-[0.14em] text-[#8a6621]">
          💡 {tr('Пример — годовщина', 'Example — anniversary')}
        </div>
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
