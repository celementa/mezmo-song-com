import * as React from "react"
import { cn } from "@/lib/utils"

interface SelectionChipProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  selected?: boolean
  label: string
  icon?: React.ReactNode
}

export function SelectionChip({ selected, label, icon, className, ...props }: SelectionChipProps) {
  const hasIcon = icon && typeof icon === 'string' ? icon.trim().length > 0 : Boolean(icon)
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-full border px-5 py-2.5 text-sm font-medium transition-all",
        selected
          ? "border-[#1A1918] bg-[#1A1918] text-white hover:opacity-90"
          : "border-[rgba(26,25,24,0.15)] bg-white text-[#1A1918] hover:border-[#1A1918] hover:shadow-sm",
        className
      )}
      {...props}
    >
      {hasIcon && <span className="text-base">{icon}</span>}
      {label}
    </button>
  )
}
