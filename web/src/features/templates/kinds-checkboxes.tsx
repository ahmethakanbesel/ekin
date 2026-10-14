import type { DeliverableKind } from "@/api/client"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldLabel } from "@/components/ui/field"
import { KindIcon } from "@/features/stories/badges"
import { m } from "@/i18n"
import { KINDS } from "./model"

/** Video / article checkboxes; the value keeps KINDS order. */
export function KindsCheckboxes({
  idPrefix,
  value,
  onChange,
  invalid,
  disabled,
}: {
  idPrefix: string
  value: DeliverableKind[]
  onChange: (next: DeliverableKind[]) => void
  invalid?: boolean
  disabled?: boolean
}) {
  return (
    <div className="flex flex-wrap gap-x-6 gap-y-2">
      {KINDS.map((k) => (
        <Field key={k} orientation="horizontal" className="w-auto">
          <Checkbox
            id={`${idPrefix}-${k}`}
            disabled={disabled}
            checked={value.includes(k)}
            aria-invalid={invalid || undefined}
            onCheckedChange={(checked) =>
              onChange(KINDS.filter((x) => (x === k ? checked : value.includes(x))))
            }
          />
          <FieldLabel htmlFor={`${idPrefix}-${k}`} className="gap-1.5 font-normal">
            <KindIcon kind={k} />
            {m.values.kind[k]}
          </FieldLabel>
        </Field>
      ))}
    </div>
  )
}
