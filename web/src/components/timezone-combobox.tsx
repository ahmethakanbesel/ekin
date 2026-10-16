import { useMemo } from "react"

import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
  ComboboxValue,
} from "@/components/ui/combobox"
import { m } from "@/i18n"
import { timezones } from "@/lib/time"
import { cn } from "@/lib/utils"

/** A searchable select over every IANA zone the browser supports. */
export function TimezoneCombobox({
  id,
  value,
  onChange,
  onBlur,
  disabled,
  invalid,
  className,
}: {
  id?: string
  value: string
  onChange: (zone: string) => void
  onBlur?: () => void
  disabled?: boolean
  invalid?: boolean
  className?: string
}) {
  const items = useMemo(() => {
    const all = timezones()
    // A zone saved elsewhere (or by an older browser) still shows.
    return value && !all.includes(value) ? [value, ...all] : all
  }, [value])

  return (
    <Combobox
      items={items}
      value={value || null}
      onValueChange={(next: string | null) => {
        if (next) onChange(next)
      }}
      onOpenChange={(open) => {
        if (!open) onBlur?.()
      }}
      disabled={disabled}
    >
      <ComboboxTrigger
        id={id}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        className={cn(
          "flex h-9 w-full items-center justify-between gap-1.5 rounded-md border border-input bg-transparent py-2 pr-2 pl-2.5 text-sm whitespace-nowrap shadow-xs transition-[color,box-shadow] dark:bg-input/30 dark:hover:bg-input/50 outline-none",
          "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring",
          "aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20",
          "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
      >
        <span className="truncate">
          <ComboboxValue />
        </span>
      </ComboboxTrigger>
      <ComboboxContent className="min-w-64">
        <ComboboxInput placeholder={m.workspace.timezoneSearch} showTrigger={false} />
        <ComboboxEmpty>{m.workspace.timezoneEmpty}</ComboboxEmpty>
        <ComboboxList>
          {(zone: string) => (
            <ComboboxItem key={zone} value={zone}>
              {zone}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
