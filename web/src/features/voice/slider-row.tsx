import { Slider } from "@/components/ui/slider"

/** A labelled slider with its value and a hint underneath. */
export function SliderRow({
  id,
  label,
  hint,
  value,
  display,
  min,
  max,
  disabled,
  onChange,
}: {
  id: string
  label: string
  hint: string
  value: number
  display: string
  min: number
  max: number
  disabled: boolean
  onChange: (value: number) => void
}) {
  return (
    <div className="grid gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <span id={id} className="text-sm font-medium">
          {label}
        </span>
        <span className="tabular text-sm text-muted-foreground">{display}</span>
      </div>
      <Slider
        aria-labelledby={id}
        thumbLabel={label}
        valueText={display}
        min={min}
        max={max}
        step={0.05}
        disabled={disabled}
        value={value}
        onValueChange={(v) => onChange(Math.round((Array.isArray(v) ? v[0] : v) * 100) / 100)}
      />
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}
