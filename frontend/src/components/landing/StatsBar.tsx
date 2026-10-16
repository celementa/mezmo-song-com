type StatItem = { val: string; label: string }

type Props = {
  items: StatItem[]
}

export function StatsBar({ items }: Props) {
  return (
    <section className="py-8 border-y border-[var(--theme-accent-soft)] bg-white/50 backdrop-blur-sm -mx-7 px-7 sm:-mx-9 sm:px-9 md:-mx-11 md:px-11">
      <div className="flex flex-wrap justify-center gap-8 md:gap-24 text-center">
        {items.map((stat, idx) => (
          <div key={idx}>
            <div className="text-3xl md:text-4xl font-bold text-[var(--theme-accent-text)] font-serif">{stat.val}</div>
            <div className="text-xs uppercase font-bold text-gray-400 tracking-wider">{stat.label}</div>
          </div>
        ))}
      </div>
    </section>
  )
}
