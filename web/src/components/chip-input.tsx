import { useRef, useState } from "react"
import { XIcon } from "lucide-react"

import { m } from "@/i18n"
import { cn } from "@/lib/utils"

/**
 * A list of short strings typed as chips: Enter, comma or leaving the
 * field adds what was typed, Backspace in the empty field removes the
 * last one, and pasting "a, b, c" adds each. Duplicates (ignoring case)
 * are dropped; once `max` is reached the field stops taking more.
 */
export function ChipInput({
  id,
  value,
  onChange,
  max,
  normalize = (s) => s.trim(),
  validate,
  placeholder,
  disabled,
  invalid,
  className,
  "aria-describedby": describedBy,
}: {
  id: string
  value: string[]
  onChange: (value: string[]) => void
  max?: number
  /** Cleans a typed item; "" drops it. */
  normalize?: (raw: string) => string
  /** A reason to refuse an item; the text then stays in the field. */
  validate?: (item: string) => string | undefined
  placeholder?: string
  disabled?: boolean
  invalid?: boolean
  className?: string
  "aria-describedby"?: string
}) {
  const [text, setText] = useState("")
  const [refusal, setRefusal] = useState<string>()
  const inputRef = useRef<HTMLInputElement>(null)
  const full = max !== undefined && value.length >= max

  /** Adds the items in raw; returns what could not be added. */
  const add = (raw: string): string => {
    const next = [...value]
    const left: string[] = []
    let reason: string | undefined
    for (const part of raw.split(/[,\n]/)) {
      const item = normalize(part)
      if (!item) continue
      if (next.some((v) => v.toLowerCase() === item.toLowerCase())) continue
      const problem = validate?.(item)
      if (problem || (max !== undefined && next.length >= max)) {
        reason ??= problem
        left.push(part.trim())
        continue
      }
      next.push(item)
    }
    if (next.length !== value.length) onChange(next)
    setRefusal(reason)
    return left.join(", ")
  }

  const remove = (i: number) => {
    onChange(value.filter((_, j) => j !== i))
    inputRef.current?.focus()
  }

  return (
    <div className="grid gap-1.5">
      <div
        className={cn(
          "flex min-h-9 w-full flex-wrap items-center gap-1.5 rounded-md border border-input bg-transparent px-2 py-1 text-base shadow-xs transition-[color,box-shadow] dark:bg-input/30 focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring md:text-sm",
          (invalid || refusal) &&
            "border-destructive ring-3 ring-destructive/20 dark:border-destructive/50 dark:ring-destructive/40",
          disabled && "cursor-not-allowed opacity-50",
          className
        )}
      >
        <ul className="contents">
          {value.map((v, i) => (
            <li
              key={v}
              className="inline-flex h-6 max-w-full items-center gap-0.5 rounded-full bg-secondary pr-2.5 pl-2.5 text-xs font-medium text-secondary-foreground has-[button]:pr-0.5"
            >
              <span className="truncate">{v}</span>
              {disabled ? null : (
                <button
                  type="button"
                  className="inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground outline-none hover:bg-foreground/10 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={m.editor.removeTag(v)}
                  onClick={() => remove(i)}
                >
                  <XIcon className="size-3" />
                </button>
              )}
            </li>
          ))}
        </ul>
        <input
          ref={inputRef}
          id={id}
          autoComplete="off"
          spellCheck={false}
          disabled={disabled}
          readOnly={full}
          aria-invalid={invalid || !!refusal || undefined}
          aria-describedby={describedBy}
          placeholder={full || disabled ? undefined : placeholder}
          className="h-7 min-w-24 flex-1 bg-transparent px-1 outline-none placeholder:text-muted-foreground read-only:cursor-default"
          value={text}
          onChange={(e) => {
            const v = e.target.value
            if (/[,\n]/.test(v)) setText(add(v))
            else {
              setText(v)
              if (refusal) setRefusal(undefined)
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              if (text.trim()) setText(add(text))
            } else if (e.key === "Backspace" && text === "" && value.length > 0) {
              e.preventDefault()
              onChange(value.slice(0, -1))
            }
          }}
          onBlur={() => {
            if (text.trim()) setText(add(text))
          }}
        />
      </div>
      {refusal ? <p className="text-sm text-destructive">{refusal}</p> : null}
    </div>
  )
}
