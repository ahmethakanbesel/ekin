import { FieldGroup } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { m } from "@/i18n"
import { Row, Section } from "./form-parts"
import type { DraftEffort, ProductionValues } from "./model"

/** The select value standing for "the workspace default" (a select item needs a value). */
const DEFAULT = "default"

/** Claude models offered for drafting; another id set through the API still shows. */
const MODELS = [
  { value: "claude-opus-5-5", label: "Claude Opus 5.5" },
  { value: "claude-sonnet-5-5", label: "Claude Sonnet 5.5" },
  { value: "claude-haiku-5-5", label: "Claude Haiku 5.5" },
  { value: "claude-fable-5-1", label: "Claude Fable 5.1" },
]

const EFFORTS: DraftEffort[] = ["low", "medium", "high", "xhigh", "max"]

/**
 * "Production" in the pipeline and request forms: how the stories are
 * made, each setting overriding the workspace default.
 */
export function ProductionSection({
  idPrefix,
  value,
  onChange,
  readOnly = false,
}: {
  idPrefix: string
  value: ProductionValues
  onChange: (next: ProductionValues) => void
  readOnly?: boolean
}) {
  const t = m.pipelineForm.production
  const models = [{ value: DEFAULT, label: t.workspaceDefault }, ...MODELS]
  if (value.draftModel && !MODELS.some((it) => it.value === value.draftModel)) {
    models.push({ value: value.draftModel, label: value.draftModel })
  }
  const efforts = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...EFFORTS.map((e) => ({ value: e, label: t.efforts[e] })),
  ]
  const set = (patch: Partial<ProductionValues>) => onChange({ ...value, ...patch })

  return (
    <Section title={t.title} description={t.description} disabled={readOnly}>
      <FieldGroup className="gap-5 sm:grid sm:grid-cols-2">
        <Row label={t.model} htmlFor={`${idPrefix}-model`} hint={t.modelHint}>
          <Select
            items={models}
            disabled={readOnly}
            value={value.draftModel || DEFAULT}
            onValueChange={(v) => v && set({ draftModel: v === DEFAULT ? "" : v })}
          >
            <SelectTrigger id={`${idPrefix}-model`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {models.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        <Row label={t.effort} htmlFor={`${idPrefix}-effort`} hint={t.effortHint}>
          <Select
            items={efforts}
            disabled={readOnly}
            value={value.draftEffort || DEFAULT}
            onValueChange={(v) =>
              v && set({ draftEffort: v === DEFAULT ? "" : (v as DraftEffort) })
            }
          >
            <SelectTrigger id={`${idPrefix}-effort`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {efforts.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
      </FieldGroup>
    </Section>
  )
}
