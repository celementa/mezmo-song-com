import * as React from "react"
import { cn } from "@/lib/utils"

interface VibeCardProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  selected?: boolean
  label: string
  description?: string
  icon?: React.ReactNode
  badge?: string
}

export function VibeCard({ selected, label, description, icon, badge, className, ...props }: VibeCardProps) {
  const hasIcon = icon && typeof icon === 'string' ? icon.trim().length > 0 : Boolean(icon)
  const hasDesc = description && typeof description === 'string' ? description.trim().length > 0 : Boolean(description)
  return (
    <button
      type="button"
      className={cn(
        "relative inline-flex items-center justify-center gap-1.5 rounded-full border px-5 py-2.5 text-sm font-medium transition-all",
        selected
          ? "border-[#1A1918] bg-[#1A1918] text-white hover:opacity-90"
          : "border-[rgba(26,25,24,0.15)] bg-white text-[#1A1918] hover:border-[#1A1918] hover:shadow-sm",
        className
      )}
      {...props}
    >
      {badge && (
        <span className="absolute -right-1 -top-2 rounded-full bg-yellow-400 px-1.5 py-0.5 text-[9px] font-bold text-black shadow-sm">
          {badge}
        </span>
      )}
      {hasIcon && <span className="text-base">{icon}</span>}
      <span>{label}</span>
      {hasDesc && <span className={cn("text-xs", selected ? "text-white/70" : "text-[rgba(26,25,24,0.5)]")}>· {description}</span>}
    </button>
  )
}
