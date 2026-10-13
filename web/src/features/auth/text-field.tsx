import type { ReactNode } from "react"

import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"

/** The slice of a TanStack Form field a text input needs. */
type TextFieldApi = {
  state: {
    value: string
    meta: { isTouched: boolean; isValid: boolean; errors: unknown[] }
  }
  handleChange: (value: string) => void
  handleBlur: () => void
}

/** A labelled input bound to a form field, with its first error once touched. */
export function TextField({
  field,
  id,
  label,
  hint,
  type = "text",
  autoComplete,
  placeholder,
  disabled,
}: {
  field: TextFieldApi
  id: string
  label: string
  hint?: ReactNode
  type?: "text" | "email" | "password"
  autoComplete?: string
  placeholder?: string
  disabled?: boolean
}) {
  const invalid = field.state.meta.isTouched && !field.state.meta.isValid
  return (
    <Field data-invalid={invalid || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type={type}
        autoComplete={autoComplete}
        placeholder={placeholder}
        disabled={disabled}
        value={field.state.value}
        onChange={(e) => field.handleChange(e.target.value)}
        onBlur={field.handleBlur}
        aria-invalid={invalid || undefined}
      />
      {invalid ? (
        <FieldError>{String(field.state.meta.errors[0])}</FieldError>
      ) : hint ? (
        <FieldDescription>{hint}</FieldDescription>
      ) : null}
    </Field>
  )
}
