import type { VoiceEngine } from "@/api/client"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { SourceChip } from "@/features/settings/layered"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { groupVoices, shortLabel } from "./model"

/** The select's value for "workspace default". */
const DEFAULT = "default"

/**
 * An engine's voices, grouped by accent and gender when there is more
 * than one group. With `defaultLabel`, "" (the workspace default) is an
 * option too.
 */
export function VoiceSelect({
  id,
  engine,
  value,
  onChange,
  disabled,
  defaultLabel,
  inherited,
  className,
}: {
  id: string
  engine: VoiceEngine
  value: string
  onChange: (voice: string) => void
  disabled?: boolean
  defaultLabel?: string
  /** The voice the layer below gives: chosen, the trigger reads as inherited. */
  inherited?: string
  className?: string
}) {
  const inheriting = inherited !== undefined && value === inherited
  const groups = groupVoices(engine.voices)
  const grouped = groups.length > 1
  const items = [
    ...(defaultLabel ? [{ value: DEFAULT, label: defaultLabel }] : []),
    ...engine.voices.map((v) => ({ value: v.id, label: v.label })),
  ]
  return (
    <Select
      items={items}
      disabled={disabled}
      value={value || (defaultLabel ? DEFAULT : null)}
      onValueChange={(v) => onChange(!v || v === DEFAULT ? "" : String(v))}
    >
      <SelectTrigger id={id} className={cn(className ?? "w-full", inheriting && "font-normal text-muted-foreground")}>
        <SelectValue>
          {(v: string) => {
            const label = items.find((it) => it.value === v)?.label ?? v
            return inheriting ? m.settings.inheritOption(label) : label
          }}
        </SelectValue>
        {inherited !== undefined && !inheriting ? <SourceChip here>{m.settings.setHere}</SourceChip> : null}
      </SelectTrigger>
      <SelectContent>
        {defaultLabel ? <SelectItem value={DEFAULT}>{defaultLabel}</SelectItem> : null}
        {groups.map((g) =>
          grouped ? (
            <SelectGroup key={g.key}>
              <SelectLabel>{g.label}</SelectLabel>
              {g.voices.map((v) => (
                <SelectItem key={v.id} value={v.id}>
                  {shortLabel(v)}
                </SelectItem>
              ))}
            </SelectGroup>
          ) : (
            g.voices.map((v) => (
              <SelectItem key={v.id} value={v.id}>
                {v.label}
              </SelectItem>
            ))
          )
        )}
      </SelectContent>
    </Select>
  )
}
