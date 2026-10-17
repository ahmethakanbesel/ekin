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
import { SourceChip } from "@/features/settings/layered"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { workspaceProductionQuery } from "@/queries/production"
import { templatesQuery } from "@/queries/templates"
import { NO_TEMPLATE, fitsKinds } from "./model"
import { MiniCard } from "./preview"

const NONE = NO_TEMPLATE
/** The select's value for "the workspace default". */
const INHERIT = "__inherit"

/**
 * A template for a pipeline or a request, or the workspace's default:
 * lists the templates for at least one of the chosen outputs. With
 * `inherits` (pipelines and requests), "" takes the workspace default and
 * NO_TEMPLATE is none; without it (the workspace default itself), "" is
 * none. A chosen template that no longer fits (the outputs changed) stays
 * selected with a warning; the server refuses it on save. `bare` leaves
 * out the label and hint, for a layered settings row.
 */
export function TemplateSelect({
  id,
  value,
  onChange,
  kinds,
  inherits = false,
  bare = false,
  describedBy,
  disabled = false,
}: {
  id: string
  value: string
  onChange: (templateId: string) => void
  kinds: DeliverableKind[]
  inherits?: boolean
  bare?: boolean
  describedBy?: string
  disabled?: boolean
}) {
  const workspace = useWorkspace()
  const list = useQuery(templatesQuery(workspace.id))
  const defaults = useQuery({ ...workspaceProductionQuery(workspace.id), enabled: inherits })
  const all = list.data ?? []
  const chosenId = value === NONE ? "" : value
  const selected = chosenId ? all.find((t) => t.id === chosenId) : undefined
  const fitting = all.filter((t) => fitsKinds(t, kinds))
  // A chosen template that does not fit stays listed until the outputs
  // change again: dropping it from the list the moment another one is
  // picked makes the select reset itself to the first item.
  const kindsKey = kinds.join(",")
  const [kept, setKept] = useState({ kindsKey, id: value })
  if (kept.kindsKey !== kindsKey) setKept({ kindsKey, id: value })
  const extra = kept.id && kept.id !== value && kept.id !== NONE ? all.find((t) => t.id === kept.id) : undefined
  const unfitting = [selected, extra].filter(
    (t, i, arr): t is Template => !!t && !fitsKinds(t, kinds) && arr.indexOf(t) === i
  )
  const options: Template[] = [...fitting, ...unfitting]
  const unfit = !!selected && !fitsKinds(selected, kinds)
  const unknown = !!chosenId && list.isSuccess && !selected
  const p = m.templates.picker
  // What "inherit" gives: the workspace default's name, or none.
  const defaultId = defaults.data?.templateId
  const inheritedName = defaultId ? (all.find((t) => t.id === defaultId)?.name ?? p.unknown) : p.none
  const inheritLabel = m.settings.inheritOption(inheritedName)

  const items = [
    ...(inherits ? [{ value: INHERIT, label: inheritLabel }] : []),
    { value: NONE, label: p.none },
    ...options.map((t) => ({ value: t.id, label: t.name })),
    ...(unknown ? [{ value: chosenId, label: p.unknown }] : []),
  ]
  const byId = new Map(options.map((t) => [t.id, t]))
  const selectValue = chosenId || (inherits && value === "" ? INHERIT : NONE)

  let hint: string = p.hint
  if (list.isError) hint = p.loadFailed

  const select = (
    <Select
      items={items}
      disabled={disabled}
      value={selectValue}
      onValueChange={(v) => {
        if (!v || v === INHERIT) onChange("")
        else if (v === NONE) onChange(inherits ? NONE : "")
        else onChange(String(v))
      }}
    >
      <SelectTrigger
        id={id}
        aria-describedby={describedBy}
        className={cn("w-full", !bare && "sm:max-w-md", selectValue === INHERIT && "text-muted-foreground")}
        aria-invalid={unfit || undefined}
      >
        <SelectValue>
          {(v: string) => {
            const t = byId.get(v)
            if (t) return <Option template={t} />
            if (v === INHERIT) return inheritLabel
            return v === NONE ? p.none : p.unknown
          }}
        </SelectValue>
        {inherits && selectValue !== INHERIT ? <SourceChip here>{m.settings.setHere}</SourceChip> : null}
      </SelectTrigger>
      <SelectContent>
        {inherits ? <SelectItem value={INHERIT}>{inheritLabel}</SelectItem> : null}
        <SelectItem value={NONE}>{p.none}</SelectItem>
        {options.map((t) => (
          <SelectItem key={t.id} value={t.id}>
            <Option template={t} unfit={!fitsKinds(t, kinds)} />
          </SelectItem>
        ))}
        {unknown ? <SelectItem value={chosenId}>{p.unknown}</SelectItem> : null}
      </SelectContent>
    </Select>
  )
  if (bare) return select

  return (
    <Row label={p.label} htmlFor={id} error={unfit ? p.unfit : undefined}>
      {select}
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
