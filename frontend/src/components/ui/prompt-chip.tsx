import * as React from "react"
import { cn } from "@/lib/utils"

interface PromptChipProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  label: string
  icon?: React.ReactNode
}

export function PromptChip({ label, icon, className, ...props }: PromptChipProps) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border bg-white px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-50 hover:text-gray-900",
        className
      )}
      {...props}
    >
      {icon}
      {label}
    </button>
  )
}
