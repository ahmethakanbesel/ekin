import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { CheckCircle2Icon, ChevronRightIcon, CircleAlertIcon, DownloadIcon, FileIcon } from "lucide-react"

import type { Revision } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { artifactTextQuery, artifactUrl } from "@/queries/stories"
import { Markdown } from "./markdown"
import type { ArtifactRef } from "./previews"

/** A change revision's change.json. */
export type ChangeManifest = {
  base: { source: string; repo?: string; branch?: string; sha?: string; dir?: string }
  files: { path: string; status: "added" | "modified" | "deleted"; additions: number; deletions: number; binary?: boolean }[]
  build?: { ok: boolean; exitCode: number; seconds: number; output: string; at: string }
}

export type DiffLine = { kind: "add" | "del" | "ctx" | "hunk" | "meta"; text: string; old?: number; new?: number }
export type DiffFile = { path: string; lines: DiffLine[]; binary: boolean }

/** Splits a unified git patch into files and numbered lines. */
export function parsePatch(patch: string): DiffFile[] {
  const files: DiffFile[] = []
  let file: DiffFile | undefined
  let oldN = 0
  let newN = 0
  for (const raw of patch.split("\n")) {
    if (raw.startsWith("diff --git ")) {
      const paths = /^diff --git a\/(.*) b\/(.*)$/.exec(raw)
      file = { path: paths ? paths[2] : raw.slice(11), lines: [], binary: false }
      files.push(file)
      continue
    }
    if (!file) continue
    if (raw.startsWith("@@")) {
      const h = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(raw)
      oldN = h ? Number(h[1]) : 0
      newN = h ? Number(h[2]) : 0
      file.lines.push({ kind: "hunk", text: raw })
    } else if (raw.startsWith("GIT binary patch") || raw.startsWith("Binary files")) {
      file.binary = true
    } else if (file.binary || raw.startsWith("index ") || raw.startsWith("--- ") || raw.startsWith("+++ ")) {
      continue
    } else if (/^(new|deleted) file mode|^old mode|^new mode|^similarity|^rename/.test(raw)) {
      file.lines.push({ kind: "meta", text: raw })
    } else if (raw.startsWith("+")) {
      file.lines.push({ kind: "add", text: raw.slice(1), new: newN++ })
    } else if (raw.startsWith("-")) {
      file.lines.push({ kind: "del", text: raw.slice(1), old: oldN++ })
    } else if (raw.startsWith(" ")) {
      file.lines.push({ kind: "ctx", text: raw.slice(1), old: oldN++, new: newN++ })
    } else if (raw.startsWith("\\")) {
      file.lines.push({ kind: "meta", text: raw })
    }
  }
  return files
}

function useText(a: ArtifactRef, name: string, enabled: boolean) {
  return useQuery({ ...artifactTextQuery(a.workspaceId, a.storyId, a.deliverableId, a.revision, name), enabled })
}

/** Lines shown per file before "show all". */
const FILE_LINES = 400

/**
 * A change for review: the agent's summary, whether the site built, and
 * the diff, file by file.
 */
export function ChangePreview({ artifacts: a, revision }: { artifacts: ArtifactRef; revision: Revision }) {
  const has = (name: string) => revision.artifacts.includes(name)
  const summary = useText(a, "summary.md", has("summary.md"))
  const manifestText = useText(a, "change.json", has("change.json"))
  const patch = useText(a, "change.patch", has("change.patch"))
  const manifest = useMemo<ChangeManifest | undefined>(() => {
    try {
      return manifestText.data ? (JSON.parse(manifestText.data) as ChangeManifest) : undefined
    } catch {
      return undefined
    }
  }, [manifestText.data])
  const files = useMemo(() => (patch.data ? parsePatch(patch.data) : []), [patch.data])

  if (!has("change.patch")) {
    return (
      <div className="rounded-xl border border-dashed px-6 py-10 text-center text-sm text-muted-foreground">
        {m.change.noPatch}
      </div>
    )
  }
  const added = manifest?.files.reduce((n, f) => n + f.additions, 0) ?? 0
  const removed = manifest?.files.reduce((n, f) => n + f.deletions, 0) ?? 0

  return (
    <div className="grid gap-6">
      {summary.data ? (
        <section className="grid gap-2">
          <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{m.change.summary}</h3>
          <Markdown source={summary.data} />
        </section>
      ) : null}

      <section className="flex flex-wrap items-center gap-2 text-sm">
        <BuildBadge build={manifest?.build} />
        {manifest ? (
          <span className="text-muted-foreground">
            {m.change.stats(manifest.files.length, added, removed)}
            {manifest.base.repo
              ? ` · ${m.change.base(manifest.base.repo, manifest.base.branch ?? "", (manifest.base.sha ?? "").slice(0, 7))}`
              : manifest.base.dir
                ? ` · ${manifest.base.dir}`
                : null}
          </span>
        ) : null}
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          render={
            <a
              href={artifactUrl(a.workspaceId, a.storyId, a.deliverableId, a.revision, "change.patch", true)}
              download
              aria-label={m.change.downloadPatch}
            />
          }
          nativeButton={false}
        >
          <DownloadIcon data-icon="inline-start" />
          {m.change.downloadPatch}
        </Button>
      </section>

      {manifest?.build && !manifest.build.ok ? (
        <pre className="max-h-64 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
          {manifest.build.output}
        </pre>
      ) : null}

      {patch.isPending ? (
        <p className="text-sm text-muted-foreground">{m.common.loading}</p>
      ) : (
        <div className="grid gap-3">
          {files.map((f) => (
            <FileDiff key={f.path} file={f} status={manifest?.files.find((x) => x.path === f.path)?.status} />
          ))}
        </div>
      )}
    </div>
  )
}

function BuildBadge({ build }: { build: ChangeManifest["build"] }) {
  if (!build) return <Badge variant="outline">{m.change.noBuild}</Badge>
  return build.ok ? (
    <Badge className="bg-success/15 text-success">
      <CheckCircle2Icon data-icon="inline-start" />
      {m.change.buildPassed(Math.round(build.seconds))}
    </Badge>
  ) : (
    <Badge className="bg-destructive/10 text-destructive">
      <CircleAlertIcon data-icon="inline-start" />
      {m.change.buildFailed(build.exitCode)}
    </Badge>
  )
}

const lineTone: Record<DiffLine["kind"], string> = {
  add: "bg-success/10",
  del: "bg-destructive/10",
  ctx: "",
  hunk: "bg-muted/60 text-muted-foreground",
  meta: "text-muted-foreground italic",
}

function FileDiff({ file, status }: { file: DiffFile; status?: string }) {
  const [open, setOpen] = useState(true)
  const [all, setAll] = useState(false)
  const lines = all ? file.lines : file.lines.slice(0, FILE_LINES)
  return (
    <div className="overflow-hidden rounded-lg border">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 bg-muted/40 px-3 py-2 text-left text-sm hover:bg-muted"
      >
        <ChevronRightIcon className={cn("size-4 shrink-0 transition-transform", open && "rotate-90")} />
        <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-mono text-xs">{file.path}</span>
        {status ? <Badge variant="outline">{m.change.fileStatus[status] ?? status}</Badge> : null}
      </button>
      {open ? (
        file.binary ? (
          <p className="px-3 py-2 text-sm text-muted-foreground">{m.change.binary}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse font-mono text-xs leading-5">
              <tbody>
                {lines.map((l, i) => (
                  <tr key={i} className={lineTone[l.kind]}>
                    <td className="w-10 px-2 text-right text-muted-foreground select-none">{l.old ?? ""}</td>
                    <td className="w-10 px-2 text-right text-muted-foreground select-none">{l.new ?? ""}</td>
                    <td className="w-4 text-center text-muted-foreground select-none">
                      {l.kind === "add" ? "+" : l.kind === "del" ? "−" : ""}
                    </td>
                    <td className="pr-3 whitespace-pre">{l.text}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!all && file.lines.length > FILE_LINES ? (
              <Button size="sm" variant="ghost" className="m-2" onClick={() => setAll(true)}>
                {m.change.showAll(file.lines.length - FILE_LINES)}
              </Button>
            ) : null}
          </div>
        )
      ) : null}
    </div>
  )
}
