import type { ReactNode } from "react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

// Layout pieces shared by the pipeline form and the ad-hoc request form.

/** The first error of a field, once the user touched it or tried to submit. */
export function shownError(
  meta: { isTouched: boolean; errors: unknown[] },
  submitted: boolean
): string | undefined {
  return (meta.isTouched || submitted) && meta.errors.length > 0 ? String(meta.errors[0]) : undefined
}

export function Section({
  title,
  description,
  disabled = false,
  children,
}: {
  title: string
  description?: string
  /** Disables the native inputs and buttons inside (Base UI controls take their own prop). */
  disabled?: boolean
  children: ReactNode
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium">{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
      </CardHeader>
      <CardContent>
        <fieldset disabled={disabled} className="contents">
          {children}
        </fieldset>
      </CardContent>
    </Card>
  )
}

export function Row({
  label,
  htmlFor,
  hint,
  error,
  className,
  children,
}: {
  label: string
  htmlFor?: string
  hint?: string
  error?: string
  className?: string
  children: ReactNode
}) {
  return (
    <Field data-invalid={!!error || undefined} className={cn("gap-2", className)}>
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {children}
      {error ? <FieldError>{error}</FieldError> : hint ? <FieldDescription>{hint}</FieldDescription> : null}
    </Field>
  )
}

export function OutputPanel({
  icon,
  title,
  hint,
  enabled,
  toggle,
  children,
}: {
  icon: ReactNode
  title: string
  hint: string
  enabled: boolean
  toggle: ReactNode
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        "rounded-3xl border p-4 transition-colors",
        enabled ? "border-primary/40 bg-primary/[0.03]" : "bg-muted/30"
      )}
    >
      <div className="flex items-start gap-3">
        <span className="mt-0.5 text-muted-foreground [&_svg]:size-5">{icon}</span>
        <div className="min-w-0 flex-1">
          <p className="font-medium">{title}</p>
          <p className="text-sm text-muted-foreground">{hint}</p>
        </div>
        {toggle}
      </div>
      {enabled ? <div className="mt-5">{children}</div> : null}
    </div>
  )
}

export function NumberInput({
  id,
  value,
  onChange,
  onBlur,
  min,
  max,
  step = 1,
  placeholder,
  invalid,
  className,
}: {
  id: string
  value: string
  onChange: (value: string) => void
  onBlur?: () => void
  min?: number
  max?: number
  step?: number
  placeholder?: string
  invalid?: boolean
  className?: string
}) {
  return (
    <Input
      id={id}
      type="number"
      inputMode="numeric"
      className={cn("tabular", className)}
      min={min}
      max={max}
      step={step}
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      aria-invalid={invalid || undefined}
    />
  )
}
