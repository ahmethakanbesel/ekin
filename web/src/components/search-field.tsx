import { useEffect, useEffectEvent, useState } from "react"
import { SearchIcon } from "lucide-react"

import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group"
import { cn } from "@/lib/utils"

/** How long typing pauses before the search runs. */
const DEBOUNCE_MS = 300

/**
 * A search box over a server-side list. It reports the trimmed text once
 * typing pauses; a value changed from outside (a link, Back) replaces
 * what is typed.
 */
export function SearchField({
  value,
  onChange,
  label,
  placeholder,
  className,
}: {
  value: string
  onChange: (q: string) => void
  /** Its accessible name (there is no visible label). */
  label: string
  placeholder: string
  className?: string
}) {
  const [text, setText] = useState(value)
  const [synced, setSynced] = useState(value)
  if (value !== synced) {
    setSynced(value)
    if (value !== text.trim()) setText(value)
  }

  const report = useEffectEvent((q: string) => onChange(q))
  useEffect(() => {
    const q = text.trim()
    if (q === value) return
    const t = window.setTimeout(() => report(q), DEBOUNCE_MS)
    return () => window.clearTimeout(t)
  }, [text, value])

  return (
    <InputGroup className={cn("w-full sm:w-64", className)}>
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      <InputGroupInput
        type="search"
        aria-label={label}
        placeholder={placeholder}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && text) {
            e.preventDefault()
            setText("")
          }
        }}
      />
    </InputGroup>
  )
}
