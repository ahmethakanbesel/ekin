import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { CheckIcon, CircleAlertIcon, LinkIcon, MinusIcon, RadarIcon } from "lucide-react"

import type { VisibilityAnswer, VisibilityPrompt, VisibilityReport } from "@/api/client"
import { EmptyState, ErrorState, TableSkeleton } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatDateTime } from "@/lib/time"
import { cn } from "@/lib/utils"
import { visibilityReportQuery } from "@/queries/visibility"

const t = m.visibility.results

/** The latest finished check: per model, the share of voice, cited sources, and every answer by prompt. */
export function Results({ prompts, version }: { prompts: VisibilityPrompt[]; version: string }) {
  const workspace = useWorkspace()
  const query = useQuery(visibilityReportQuery(workspace.id, "", version))
  if (query.isPending) return <TableSkeleton rows={4} />
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const rep = query.data
  if (!rep.run) return <EmptyState icon={<RadarIcon />} title={t.emptyTitle} body={t.emptyBody} />
  return <Report rep={rep} prompts={prompts} />
}

function Report({ rep, prompts }: { rep: VisibilityReport; prompts: VisibilityPrompt[] }) {
  const workspace = useWorkspace()
  const run = rep.run!
  const [open, setOpen] = useState<VisibilityAnswer>()
  const models = rep.models.map((x) => x.model)
  const byPrompt = useMemo(() => {
    const map = new Map<string, Map<string, VisibilityAnswer>>()
    for (const a of rep.answers) {
      if (!map.has(a.promptId)) map.set(a.promptId, new Map())
      map.get(a.promptId)!.set(a.model, a)
    }
    return map
  }, [rep.answers])
  const asked = prompts.filter((p) => byPrompt.has(p.id))
  const answered = rep.models.reduce((n, x) => n + x.answers, 0)
  const mentioned = rep.models.reduce((n, x) => n + x.mentioned, 0)
  const totalVoice = rep.voices.reduce((n, v) => n + v.mentions, 0)

  return (
    <div className="grid gap-6">
      <p className="text-sm text-muted-foreground">
        {t.runLine(formatDateTime(run.finishedAt ?? run.createdAt, workspace.timezone), run.done, run.failed, run.cost.toFixed(3))}
        {run.status === "capped" ? ` ${t.capped}` : null}
      </p>
      <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label={t.mentionRate} value={answered ? `${Math.round((mentioned / answered) * 100)}%` : "—"} hint={t.mentionRateHint(mentioned, answered)} />
        {rep.models.map((x) => (
          <Stat
            key={x.model}
            label={x.model}
            value={x.answers ? `${x.mentioned}/${x.answers}` : "—"}
            hint={x.mentioned ? t.modelHint(x.avgPosition.toFixed(1), x.cited) : t.never}
            mono
          />
        ))}
      </dl>

      <div className="grid gap-6 xl:grid-cols-2">
        <section>
          <h3 className="mb-2 text-sm font-semibold">{t.voice}</h3>
          <ul className="grid gap-2">
            {rep.voices.map((v) => (
              <li key={v.name} className="grid grid-cols-[8rem_1fr_3rem] items-center gap-3 text-sm">
                <span className={cn("truncate", v.own && "font-semibold")}>{v.name}</span>
                <span className="h-2 overflow-hidden rounded-full bg-muted">
                  <span
                    className={cn("block h-full rounded-full", v.own ? "bg-primary" : "bg-muted-foreground/40")}
                    style={{ width: totalVoice ? `${(v.mentions / totalVoice) * 100}%` : 0 }}
                  />
                </span>
                <span className="tabular text-right text-muted-foreground">{v.mentions}</span>
              </li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-muted-foreground">{t.voiceHint}</p>
        </section>
        <section>
          <h3 className="mb-2 text-sm font-semibold">{t.domains}</h3>
          {rep.domains.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t.noCitations}</p>
          ) : (
            <ul className="grid gap-1 text-sm">
              {rep.domains.map((d) => (
                <li key={d.domain} className="flex justify-between gap-3">
                  <span className="truncate font-mono text-xs">{d.domain}</span>
                  <span className="tabular text-muted-foreground">{d.count}</span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      <section className="min-w-0">
        <h3 className="mb-2 text-sm font-semibold">{t.byPrompt}</h3>
        <div className="overflow-x-auto rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t.prompt}</TableHead>
                {models.map((x) => (
                  <TableHead key={x} className="text-center font-mono text-xs">
                    {x}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {asked.map((p) => (
                <TableRow key={p.id}>
                  <TableCell className="max-w-96 whitespace-normal">{p.text}</TableCell>
                  {models.map((x) => {
                    const a = byPrompt.get(p.id)?.get(x)
                    return (
                      <TableCell key={x} className="text-center">
                        {a ? <Cell answer={a} onOpen={() => setOpen(a)} /> : null}
                      </TableCell>
                    )
                  })}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>
      <AnswerDialog answer={open} prompt={prompts.find((p) => p.id === open?.promptId)} onClose={() => setOpen(undefined)} />
    </div>
  )
}

function Stat({ label, value, hint, mono = false }: { label: string; value: string; hint: string; mono?: boolean }) {
  return (
    <div className="min-w-0 rounded-xl border px-4 py-3">
      <dt className={cn("truncate text-xs text-muted-foreground", mono && "font-mono")} title={label}>
        {label}
      </dt>
      <dd className="tabular mt-1 text-2xl font-semibold">{value}</dd>
      <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}

function Cell({ answer: a, onOpen }: { answer: VisibilityAnswer; onOpen: () => void }) {
  let content
  let label
  if (a.error) {
    content = <CircleAlertIcon className="size-4 text-destructive" />
    label = t.failed
  } else if (a.mentioned) {
    content = (
      <span className="inline-flex items-center gap-1 text-success">
        <CheckIcon className="size-4" />
        <span className="tabular text-xs">#{a.position}</span>
        {a.cited ? <LinkIcon className="size-3" /> : null}
      </span>
    )
    label = t.named(a.position, a.cited)
  } else {
    content = <MinusIcon className="size-4 text-muted-foreground" />
    label = a.competitors.length ? t.missingFor(a.competitors.join(", ")) : t.missing
  }
  return (
    <button type="button" onClick={onOpen} title={label} aria-label={label} className="rounded-md p-1 hover:bg-muted">
      {content}
    </button>
  )
}

function AnswerDialog({ answer: a, prompt, onClose }: { answer?: VisibilityAnswer; prompt?: VisibilityPrompt; onClose: () => void }) {
  return (
    <Dialog open={!!a} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        {a ? (
          <>
            <DialogHeader>
              <DialogTitle className="font-mono text-sm">{a.model}</DialogTitle>
              <DialogDescription>{prompt?.text}</DialogDescription>
            </DialogHeader>
            <div className="flex flex-wrap gap-2">
              {a.mentioned ? <Badge className="bg-success/15 text-success">{t.named(a.position, a.cited)}</Badge> : <Badge variant="outline">{t.missing}</Badge>}
              {a.competitors.map((c) => (
                <Badge key={c} variant="outline">
                  {c}
                </Badge>
              ))}
            </div>
            {a.error ? (
              <p className="text-sm text-destructive">{a.error}</p>
            ) : a.text ? (
              <p className="max-h-80 overflow-y-auto text-sm whitespace-pre-wrap">{a.text}</p>
            ) : (
              <p className="text-sm text-muted-foreground">{t.textForgotten}</p>
            )}
            {a.citations.length > 0 ? (
              <div>
                <h4 className="mb-1 text-xs font-medium text-muted-foreground">{t.citations}</h4>
                <ul className="grid gap-0.5">
                  {a.citations.map((c) => (
                    <li key={c} className="truncate">
                      <a href={c} target="_blank" rel="noopener noreferrer" className="font-mono text-xs hover:underline">
                        {c}
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
