import { useState } from "react"
import { PlusIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import type { VisibilityBrand, VisibilitySettings } from "@/api/client"
import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { Row } from "@/features/pipelines/form-parts"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useSaveVisibility } from "@/queries/visibility"

const t = m.visibility.settings
const SCHEDULES = ["weekly", "daily", "off"] as const
type Schedule = (typeof SCHEDULES)[number]

type BrandRow = { name: string; domain: string }

const list = (s: string) =>
  s
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean)

/** The brand, its competitors, the models asked, how often and within what cap. Owners edit. */
export function SettingsForm({ initial, onSaved }: { initial: VisibilitySettings; onSaved?: () => void }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveVisibility(workspace.id)
  const [name, setName] = useState(initial.brand.name)
  const [aliases, setAliases] = useState((initial.brand.aliases ?? []).join(", "))
  const [domain, setDomain] = useState(initial.brand.domain ?? "")
  const [competitors, setCompetitors] = useState<BrandRow[]>(
    initial.competitors.map((c) => ({ name: c.name, domain: c.domain ?? "" }))
  )
  const [models, setModels] = useState(initial.models.join("\n"))
  const [schedule, setSchedule] = useState<Schedule>((initial.schedule as Schedule) ?? "weekly")
  const [cap, setCap] = useState(String(initial.monthlyCap ?? 500))

  const submit = async () => {
    const brand: VisibilityBrand = { name: name.trim(), aliases: list(aliases), domain: domain.trim() || undefined }
    try {
      await save.mutateAsync({
        brand,
        competitors: competitors
          .filter((c) => c.name.trim())
          .map((c) => ({ name: c.name.trim(), domain: c.domain.trim() || undefined })),
        models: list(models),
        schedule,
        monthlyCap: Number.parseInt(cap, 10) || undefined,
      })
      toast.success(t.saved)
      onSaved?.()
    } catch {
      // shown in the banner
    }
  }

  return (
    <form
      noValidate
      className="grid max-w-3xl gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <fieldset disabled={!canManage} className="contents">
        <section className="grid gap-4">
          <h3 className="font-semibold">{t.brand}</h3>
          <div className="grid gap-4 sm:grid-cols-2">
            <Row label={t.name} htmlFor="vis-name" hint={t.nameHint}>
              <Input id="vis-name" value={name} onChange={(e) => setName(e.target.value)} />
            </Row>
            <Row label={t.domain} htmlFor="vis-domain" hint={t.domainHint}>
              <Input id="vis-domain" className="font-mono" value={domain} placeholder="example.com" onChange={(e) => setDomain(e.target.value)} />
            </Row>
          </div>
          <Row label={t.aliases} htmlFor="vis-aliases" hint={t.aliasesHint}>
            <Input id="vis-aliases" value={aliases} onChange={(e) => setAliases(e.target.value)} />
          </Row>
        </section>

        <section className="grid gap-3">
          <h3 className="font-semibold">{t.competitors}</h3>
          <p className="text-sm text-muted-foreground">{t.competitorsHint}</p>
          {competitors.map((c, i) => (
            <div key={i} className="grid grid-cols-[1fr_1fr_auto] gap-2">
              <Input
                aria-label={t.competitorName}
                placeholder={t.competitorName}
                value={c.name}
                onChange={(e) => setCompetitors((rows) => rows.map((r, j) => (j === i ? { ...r, name: e.target.value } : r)))}
              />
              <Input
                aria-label={t.competitorDomain}
                placeholder="competitor.com"
                className="font-mono"
                value={c.domain}
                onChange={(e) => setCompetitors((rows) => rows.map((r, j) => (j === i ? { ...r, domain: e.target.value } : r)))}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t.removeCompetitor}
                onClick={() => setCompetitors((rows) => rows.filter((_, j) => j !== i))}
              >
                <Trash2Icon />
              </Button>
            </div>
          ))}
          {competitors.length < 10 ? (
            <div>
              <Button type="button" variant="outline" size="sm" onClick={() => setCompetitors((rows) => [...rows, { name: "", domain: "" }])}>
                <PlusIcon data-icon="inline-start" />
                {t.addCompetitor}
              </Button>
            </div>
          ) : null}
        </section>

        <section className="grid gap-4">
          <h3 className="font-semibold">{t.checks}</h3>
          <Row label={t.models} htmlFor="vis-models" hint={t.modelsHint}>
            <Textarea id="vis-models" rows={4} className="font-mono text-[13px]" value={models} onChange={(e) => setModels(e.target.value)} />
          </Row>
          <div className="grid gap-4 sm:grid-cols-2">
            <Row label={t.schedule} htmlFor="vis-schedule">
              <Select
                items={SCHEDULES.map((s) => ({ value: s, label: t.schedules[s] }))}
                value={schedule}
                disabled={!canManage}
                onValueChange={(v) => typeof v === "string" && setSchedule(v as Schedule)}
              >
                <SelectTrigger id="vis-schedule" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SCHEDULES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {t.schedules[s]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Row>
            <Row label={t.cap} htmlFor="vis-cap" hint={t.capHint}>
              <Input id="vis-cap" inputMode="numeric" className="tabular" value={cap} onChange={(e) => setCap(e.target.value)} />
            </Row>
          </div>
        </section>
      </fieldset>
      {save.isError ? <Banner title={errorMessage(save.error)} /> : null}
      {canManage ? (
        <div>
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? m.common.saving : t.save}
          </Button>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{t.readOnly}</p>
      )}
    </form>
  )
}
