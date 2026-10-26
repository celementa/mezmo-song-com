import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'

import { COMPANY, LEGAL_LINKS, type LegalDoc } from '@/lib/site'
import { localePath, useLang, useTr } from '@/lib/lang'
import { openCookieSettings } from '@/lib/consent'

/**
 * Юридические страницы mezmo-song.com. Тексты живут в src/legal/*.md — их
 * правят юристы и менеджеры, поэтому источник — markdown, а не JSX. Разметка
 * нужна минимальная (заголовки, абзацы, списки, жирный, ссылки, черта), и под
 * неё хватает маленького рендера ниже без библиотеки. Английские версии —
 * src/legal/en/*.md (их прислала заказчица, в них есть таблицы).
 */
const DOCS = import.meta.glob<string>(['../legal/*.md', '../legal/en/*.md'], { query: '?raw', import: 'default', eager: true })

export function LegalPage({ doc }: { doc: LegalDoc }) {
  const lang = useLang()
  const tr = useTr()
  const source = DOCS[lang === 'en' ? `../legal/en/${doc}.md` : `../legal/${doc}.md`] ?? ''

  return (
    <div className="min-h-screen bg-white">
      <div className="mx-auto max-w-3xl px-4 py-12 sm:py-16">
        <Link to={localePath('/')} className="mb-8 inline-flex items-center gap-1.5 text-sm text-gray-400 transition-colors hover:text-gray-600">
          <ArrowLeft className="h-4 w-4" />
          {tr('На главную', 'Back to home')}
        </Link>

        <article className="space-y-4 text-[15px] leading-relaxed text-gray-600">{renderMarkdown(source)}</article>

        <footer className="mt-12 border-t border-gray-100 pt-6 text-xs leading-relaxed text-gray-400">
          <div>
            {COMPANY.name} · {tr('Рег. №', 'Reg. No.')} {COMPANY.regNo} · {COMPANY.address} ·{' '}
            <a href={`mailto:${COMPANY.email}`} className="hover:text-gray-600">{COMPANY.email}</a>
          </div>
          <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1">
            {LEGAL_LINKS.map((l) => (
              <Link key={l.doc} to={localePath(`/${l.doc}`)} className="hover:text-gray-600">{l.label[lang]}</Link>
            ))}
            <button type="button" onClick={openCookieSettings} className="hover:text-gray-600">{tr('Настройки cookie', 'Cookie settings')}</button>
          </div>
        </footer>
      </div>
    </div>
  )
}

function renderMarkdown(md: string): ReactNode[] {
  const out: ReactNode[] = []
  let para: string[] = []
  let list: string[] = []
  let table: string[][] = []

  const flushPara = () => {
    if (para.length) {
      out.push(
        <p key={out.length}>
          {para.map((line, i) => (
            <span key={i}>
              {i > 0 && <br />}
              {inline(line)}
            </span>
          ))}
        </p>,
      )
    }
    para = []
  }
  const flushList = () => {
    if (list.length) {
      out.push(
        <ul key={out.length} className="list-disc space-y-1 pl-5">
          {list.map((item, i) => <li key={i}>{inline(item)}</li>)}
        </ul>,
      )
    }
    list = []
  }

  const flushTable = () => {
    if (table.length) {
      const [head, ...rows] = table
      out.push(
        <div key={out.length} className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-sm">
            <thead>
              <tr>{head.map((cell, i) => <th key={i} className="border-b border-gray-200 py-2 pr-4 font-semibold text-gray-800">{inline(cell)}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((row, r) => (
                <tr key={r}>{row.map((cell, i) => <td key={i} className="border-b border-gray-100 py-2 pr-4 align-top">{inline(cell)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>,
      )
    }
    table = []
  }

  for (const line of md.split('\n')) {
    // | a | b | — строка таблицы; | :--- | — разделитель шапки, пропускаем.
    if (line.startsWith('|')) {
      flushPara()
      flushList()
      const cells = line.slice(1, line.endsWith('|') ? -1 : undefined).split('|').map((c) => c.trim())
      if (!cells.every((c) => /^:?-+:?$/.test(c))) table.push(cells)
      continue
    }
    flushTable()
    if (line.startsWith('- ')) {
      flushPara()
      list.push(line.slice(2))
      continue
    }
    flushList()
    const heading = /^(#{1,3}) (.*)$/.exec(line)
    if (heading) {
      flushPara()
      const text = inline(heading[2])
      if (heading[1].length === 1) out.push(<h1 key={out.length} className="!mb-6 text-3xl font-bold text-gray-900">{text}</h1>)
      else out.push(<h2 key={out.length} className="!mt-8 text-lg font-semibold text-gray-800">{text}</h2>)
    } else if (line.trim() === '---') {
      flushPara()
      out.push(<hr key={out.length} className="!my-8 border-gray-100" />)
    } else if (!line.trim()) {
      flushPara()
    } else {
      para.push(line)
    }
  }
  flushPara()
  flushList()
  flushTable()
  return out
}

/**
 * **жирный** и [текст](ссылка) — всё, что встречается в документах. Внутренние
 * ссылки ведут на документ того же языка, внешние и mailto: — обычные <a>.
 */
function inline(text: string): ReactNode[] {
  const parts: ReactNode[] = []
  const re = /\*\*(.+?)\*\*|\[([^\]]+)\]\(([^)]+)\)/g
  let last = 0
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) parts.push(text.slice(last, m.index))
    if (m[1] !== undefined) {
      parts.push(<strong key={m.index} className="font-semibold text-gray-800">{m[1]}</strong>)
    } else {
      const href = m[3]
      parts.push(
        href.startsWith('/') ? (
          <Link key={m.index} to={localePath(href)} className="text-rose-600 hover:underline">{m[2]}</Link>
        ) : (
          <a key={m.index} href={href} target={href.startsWith('http') ? '_blank' : undefined} rel="noopener noreferrer" className="text-rose-600 hover:underline">{m[2]}</a>
        ),
      )
    }
    last = m.index + m[0].length
  }
  if (last < text.length) parts.push(text.slice(last))
  return parts
}
