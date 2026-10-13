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
          "flex h-9 w-full items-center justify-between gap-1.5 rounded-3xl border border-transparent bg-input/50 px-3 py-2 text-sm whitespace-nowrap transition-[color,box-shadow,background-color] outline-none",
          "focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30",
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
