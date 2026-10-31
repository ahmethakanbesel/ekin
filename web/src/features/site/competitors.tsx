import { useState } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"
import { KeyRoundIcon, RefreshCwIcon, Trash2Icon, UsersIcon, WrenchIcon } from "lucide-react"
import { toast } from "sonner"

import type { Competitor, CompetitorChange } from "@/api/client"
import { EmptyState, ErrorState, LoadMore, TableSkeleton } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatAgo, formatDateTime, useNow } from "@/lib/time"
import { useImportOpportunities } from "@/queries/seo"
import {
  competitorChangesQuery,
  competitorsQuery,
  useAddCompetitor,
  useCheckCompetitor,
  useRemoveCompetitor,
} from "@/queries/site"
import { ChangeDialog } from "./change-dialog"
import { shortURL } from "./rules"

const t = m.site.competitors
type Kind = "added" | "removed" | "all"

/** The competitors followed, and the pages they added or dropped, as signals. */
export function CompetitorsView() {
  const workspace = useWorkspace()
  const query = useQuery(competitorsQuery(workspace.id))
  if (query.isPending) return <TableSkeleton rows={3} />
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  const list = query.data.competitors
  return (
    <div className="grid gap-8">
      <CompetitorList competitors={list} />
      {list.length > 0 ? <Changes competitors={list} /> : null}
    </div>
  )
}

function CompetitorList({ competitors }: { competitors: Competitor[] }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const add = useAddCompetitor(workspace.id)
  const [url, setUrl] = useState("")
  const [name, setName] = useState("")
  const submit = async () => {
    try {
      await add.mutateAsync({ url: url.trim(), name: name.trim() || undefined })
      setUrl("")
      setName("")
      toast.success(t.added)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">{t.title}</h3>
        <p className="text-sm text-muted-foreground">{t.hint}</p>
      </div>
      {competitors.length === 0 ? (
        <EmptyState icon={<UsersIcon />} title={t.emptyTitle} body={canManage ? t.emptyBody : t.emptyViewer} />
      ) : (
        <ul className="divide-y rounded-xl border">
          {competitors.map((c) => (
            <CompetitorRow key={c.id} competitor={c} />
          ))}
        </ul>
      )}
      {canManage && competitors.length < 10 ? (
        <form
          className="flex flex-wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <Input aria-label={t.url} placeholder="competitor.com" className="max-w-64 font-mono" value={url} onChange={(e) => setUrl(e.target.value)} />
          <Input aria-label={t.name} placeholder={t.name} className="max-w-48" value={name} onChange={(e) => setName(e.target.value)} />
          <Button type="submit" variant="outline" disabled={!url.trim() || add.isPending}>
            {t.add}
          </Button>
        </form>
      ) : null}
    </section>
  )
}

function CompetitorRow({ competitor: c }: { competitor: Competitor }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const canEdit = useCan("edit")
  const check = useCheckCompetitor(workspace.id)
  const remove = useRemoveCompetitor(workspace.id)
  const now = useNow()
  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{c.name}</p>
        <p className="text-xs text-muted-foreground">
          <a href={c.url} target="_blank" rel="noopener noreferrer" className="font-mono hover:underline">
            {c.url}
          </a>
          {" · "}
          {c.lastCheckAt ? (
            <span title={formatDateTime(c.lastCheckAt, workspace.timezone)}>{t.checked(c.pages, formatAgo(c.lastCheckAt, now))}</span>
          ) : (
            t.firstCheck
          )}
        </p>
        {c.lastError ? <p className="mt-0.5 text-xs text-destructive">{c.lastError}</p> : null}
      </div>
      {canEdit ? (
        <Button variant="ghost" size="sm" disabled={check.isPending} onClick={() => void run(() => check.mutateAsync(c.id), t.checking)}>
          <RefreshCwIcon data-icon="inline-start" />
          {t.checkNow}
        </Button>
      ) : null}
      {canManage ? (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t.remove}
          disabled={remove.isPending}
          onClick={() => void run(() => remove.mutateAsync(c.id), t.removed)}
        >
          <Trash2Icon />
        </Button>
      ) : null}
    </li>
  )
}

/** Runs fn, then says ok or what went wrong. */
async function run(fn: () => Promise<unknown>, ok: string) {
  try {
    await fn()
    toast.success(ok)
  } catch (err) {
    toast.error(errorMessage(err))
  }
}

function Changes({ competitors }: { competitors: Competitor[] }) {
  const workspace = useWorkspace()
  const [kind, setKind] = useState<Kind>("added")
  const list = useInfiniteQuery(competitorChangesQuery(workspace.id, kind))
  const names = new Map(competitors.map((c) => [c.id, c]))
  return (
    <section className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">{t.changes}</h3>
          <p className="text-sm text-muted-foreground">{t.changesHint}</p>
        </div>
        <Tabs value={kind} onValueChange={(v) => setKind(v as Kind)}>
          <TabsList aria-label={t.changes}>
            <TabsTrigger value="added">{t.kinds.added}</TabsTrigger>
            <TabsTrigger value="removed">{t.kinds.removed}</TabsTrigger>
            <TabsTrigger value="all">{t.kinds.all}</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      {list.isPending ? (
        <TableSkeleton rows={3} />
      ) : list.isLoadingError ? (
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : list.data.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">{t.noChanges}</p>
      ) : (
        <>
          <ul className="divide-y rounded-xl border">
            {list.data.map((ch) => (
              <ChangeRow key={ch.id} change={ch} competitor={names.get(ch.competitorId)} />
            ))}
          </ul>
          <LoadMore query={list} />
        </>
      )}
    </section>
  )
}

/** A search keyword from a page: its title without the site's name, else its path's words. */
function keywordOf(ch: CompetitorChange, competitor?: Competitor): string {
  let title = ch.title
  if (competitor) {
    for (const sep of [" | ", " — ", " – ", " - ", " · "]) {
      const parts = title.split(sep)
      if (parts.length > 1 && parts.at(-1)!.toLowerCase().includes(competitor.name.toLowerCase().split(".")[0])) {
        title = parts.slice(0, -1).join(sep)
      }
    }
  }
  if (title.trim()) return title.trim().toLowerCase().slice(0, 120)
  try {
    const last = new URL(ch.url).pathname.split("/").filter(Boolean).at(-1) ?? ""
    return decodeURIComponent(last).replace(/\.[a-z]+$/, "").replace(/[-_]+/g, " ").trim().slice(0, 120)
  } catch {
    return ""
  }
}

function ChangeRow({ change: ch, competitor }: { change: CompetitorChange; competitor?: Competitor }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const now = useNow()
  const importKeyword = useImportOpportunities(workspace.id)
  const [changing, setChanging] = useState(false)
  const name = competitor?.name ?? t.unknown
  const keyword = keywordOf(ch, competitor)
  const addKeyword = async () => {
    try {
      const added = await importKeyword.mutateAsync([{ keyword, note: t.keywordNote(name, ch.url) }])
      toast.success(added ? t.keywordAdded(keyword) : t.keywordKnown(keyword))
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  return (
    <li className="flex flex-wrap items-start gap-3 px-4 py-3">
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline" className={ch.kind === "added" ? "bg-info/10 text-info" : "text-muted-foreground"}>
            {t.kind[ch.kind]}
          </Badge>
          <span className="text-sm font-medium">{name}</span>
          <span className="text-xs text-muted-foreground" title={formatDateTime(ch.at, workspace.timezone)}>
            {formatAgo(ch.at, now)}
          </span>
        </div>
        {ch.title ? <p className="text-sm">{ch.title}</p> : null}
        <a href={ch.url} target="_blank" rel="noopener noreferrer" title={ch.url} className="block truncate font-mono text-xs text-muted-foreground hover:underline">
          {shortURL(ch.url, competitor?.url)}
        </a>
      </div>
      {canEdit && ch.kind === "added" ? (
        <div className="flex flex-wrap gap-1">
          <Button variant="ghost" size="sm" disabled={!keyword || importKeyword.isPending} onClick={() => void addKeyword()} title={t.keywordHint(keyword)}>
            <KeyRoundIcon data-icon="inline-start" />
            {t.addKeyword}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setChanging(true)}>
            <WrenchIcon data-icon="inline-start" />
            {t.answer}
          </Button>
          <ChangeDialog open={changing} onOpenChange={setChanging} initial={t.answerInstructions(name, ch.title || ch.url, ch.url)} />
        </div>
      ) : null}
    </li>
  )
}
