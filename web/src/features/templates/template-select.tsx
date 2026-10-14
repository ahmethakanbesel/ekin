import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import type { DeliverableKind, Template } from "@/api/client"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Row } from "@/features/pipelines/form-parts"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { templatesQuery } from "@/queries/templates"
import { fitsKinds } from "./model"
import { MiniCard } from "./preview"

/** The select's value for "no template". */
const NONE = "none"

/**
 * Optional template for a pipeline or a request: lists the templates for
 * at least one of the chosen outputs. "" means none. A chosen template
 * that no longer fits (the outputs changed) stays selected with a warning;
 * the server refuses it on save.
 */
export function TemplateSelect({
  id,
  value,
  onChange,
  kinds,
  disabled = false,
}: {
  id: string
  value: string
  onChange: (templateId: string) => void
  kinds: DeliverableKind[]
  disabled?: boolean
}) {
  const workspace = useWorkspace()
  const list = useQuery(templatesQuery(workspace.id))
  const all = list.data ?? []
  const selected = value ? all.find((t) => t.id === value) : undefined
  const fitting = all.filter((t) => fitsKinds(t, kinds))
  // A chosen template that does not fit stays listed until the outputs
  // change again: dropping it from the list the moment another one is
  // picked makes the select reset itself to the first item.
  const kindsKey = kinds.join(",")
  const [kept, setKept] = useState({ kindsKey, id: value })
  if (kept.kindsKey !== kindsKey) setKept({ kindsKey, id: value })
  const extra = kept.id && kept.id !== value ? all.find((t) => t.id === kept.id) : undefined
  const unfitting = [selected, extra].filter(
    (t, i, arr): t is Template => !!t && !fitsKinds(t, kinds) && arr.indexOf(t) === i
  )
  const options: Template[] = [...fitting, ...unfitting]
  const unfit = !!selected && !fitsKinds(selected, kinds)
  const unknown = !!value && list.isSuccess && !selected
  const p = m.templates.picker

  const items = [
    { value: NONE, label: p.none },
    ...options.map((t) => ({ value: t.id, label: t.name })),
    ...(unknown ? [{ value, label: p.unknown }] : []),
  ]
  const byId = new Map(options.map((t) => [t.id, t]))

  let hint: string = p.hint
  if (list.isError) hint = p.loadFailed

  return (
    <Row label={p.label} htmlFor={id} error={unfit ? p.unfit : undefined}>
      <Select
        items={items}
        disabled={disabled}
        value={value || NONE}
        onValueChange={(v) => onChange(!v || v === NONE ? "" : String(v))}
      >
        <SelectTrigger id={id} className="w-full sm:max-w-md" aria-invalid={unfit || undefined}>
          <SelectValue>
            {(v: string) => {
              const t = byId.get(v)
              if (t) return <Option template={t} />
              return v === NONE ? p.none : p.unknown
            }}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>{p.none}</SelectItem>
          {options.map((t) => (
            <SelectItem key={t.id} value={t.id}>
              <Option template={t} unfit={!fitsKinds(t, kinds)} />
            </SelectItem>
          ))}
          {unknown ? <SelectItem value={value}>{p.unknown}</SelectItem> : null}
        </SelectContent>
      </Select>
      <p className="text-sm text-muted-foreground">
        {hint}{" "}
        <Link
          to="/w/$workspaceId/templates"
          params={{ workspaceId: workspace.id }}
          className="font-medium text-foreground underline-offset-4 hover:underline"
        >
          {p.browse}
        </Link>
      </p>
    </Row>
  )
}

function Option({ template, unfit = false }: { template: Template; unfit?: boolean }) {
  return (
    <span className="flex min-w-0 items-center gap-2.5">
      <MiniCard template={template} />
      <span className="truncate">{template.name}</span>
      <span className="truncate text-xs font-normal text-muted-foreground">
        {unfit ? m.templates.picker.unfitOption : m.templates.source[template.source]}
      </span>
    </span>
  )
}
