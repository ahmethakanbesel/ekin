import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { Section } from "./form-parts"

/** Rounds of self-review offered: off, then one to five. */
const ROUNDS = [0, 1, 2, 3, 4, 5]

/** The default when a pipeline or request does not say. */
export const DEFAULT_SELF_REVIEW = 1

/**
 * How many times, at most, a fresh agent reviews each draft and the author
 * fixes what it finds, before a person sees it.
 */
export function SelfReviewSection({
  value,
  onChange,
  readOnly = false,
}: {
  value: number
  onChange: (rounds: number) => void
  readOnly?: boolean
}) {
  const t = m.pipelineForm.selfReview
  return (
    <Section title={t.title} description={t.description} disabled={readOnly}>
      <div className="flex flex-wrap gap-1.5" role="radiogroup" aria-label={t.title}>
        {ROUNDS.map((n) => (
          <Button
            key={n}
            type="button"
            size="sm"
            variant="outline"
            role="radio"
            aria-checked={value === n}
            className="aria-checked:bg-secondary aria-checked:text-foreground aria-checked:inset-ring aria-checked:inset-ring-foreground/70"
            onClick={() => onChange(n)}
          >
            {n === 0 ? t.off : t.rounds(n)}
          </Button>
        ))}
      </div>
      <p className="mt-3 text-xs text-muted-foreground">
        {value === 0 ? t.offHint : t.hint(value)}
      </p>
    </Section>
  )
}
