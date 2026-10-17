import { useRef, useState } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import {
  AlertCircleIcon,
  ArchiveIcon,
  FilmIcon,
  ImageIcon,
  ImagesIcon,
  Loader2Icon,
  MusicIcon,
  PauseIcon,
  PlayIcon,
  SearchIcon,
  UploadIcon,
} from "lucide-react"

import type { LibraryAsset } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  LoadMore,
  PageHeader,
  ReadOnlyNote,
} from "@/components/page"
import { SearchField } from "@/components/search-field"
import { DropOverlay, UploadQueue, useFileDrop } from "@/components/uploads"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatDuration } from "@/lib/time"
import { cn } from "@/lib/utils"
import { libraryFileUrl, libraryQuery } from "@/queries/library"
import { AssetSheet } from "./asset-sheet"
import { LIBRARY_TABS, type LibrarySearch, type LibraryTab } from "./search"
import { ACCEPTED_EXTENSIONS, useUploads } from "./use-uploads"

const ROUTE = "/w/$workspaceId/library"

export const kindIcon = { music: MusicIcon, video: FilmIcon, photo: ImageIcon }

export function LibraryPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: LibraryTab = search.kind ?? "all"
  const q = search.q ?? ""
  const archived = search.archived ?? false
  const list = useInfiniteQuery(
    libraryQuery(workspace.id, { kind: tab, q, archived })
  )
  const uploads = useUploads(workspace.id)
  const input = useRef<HTMLInputElement>(null)
  const drop = useFileDrop(canEdit, uploads.add)
  const player = useAudioPlayer()
  const TabIcon = tab === "all" ? ArchiveIcon : kindIcon[tab]

  const setSearch = (patch: Partial<LibrarySearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })

  const uploadButton = canEdit ? (
    <Button onClick={() => input.current?.click()}>
      <UploadIcon data-icon="inline-start" />
      {m.library.upload}
    </Button>
  ) : null

  let body
  if (list.isPending) body = <GridSkeleton />
  else if (list.isLoadingError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0 && q)
    body = (
      <EmptyState
        icon={<SearchIcon />}
        title={m.common.noMatch(q)}
        body={m.common.noMatchBody}
      />
    )
  else if (list.data.length === 0 && (tab !== "all" || archived))
    body = (
      <EmptyState
        icon={<TabIcon />}
        title={
          tab === "all" ? m.library.emptyArchived : m.library.emptyKind[tab]
        }
        body={canEdit ? m.library.dropHint : m.library.emptyBodyReadOnly}
      />
    )
  else if (list.data.length === 0)
    body = (
      <EmptyState
        icon={<ImagesIcon />}
        title={m.library.emptyTitle}
        body={canEdit ? m.library.emptyBody : m.library.emptyBodyReadOnly}
        action={
          canEdit ? (
            <div className="flex flex-col items-center gap-2">
              {uploadButton}
              <p className="text-xs text-muted-foreground">
                {m.library.dropHint}
              </p>
            </div>
          ) : null
        }
      />
    )
  else
    body = (
      <>
        <ul
          aria-busy={list.isPlaceholderData || undefined}
          className={cn(
            "grid gap-4 transition-opacity sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4",
            list.isPlaceholderData && "opacity-60"
          )}
        >
          {list.data.map((a) => (
            <li key={a.id}>
              <AssetCard
                workspaceId={workspace.id}
                asset={a}
                playing={player.playing === a.id}
                onTogglePlay={() =>
                  player.toggle(a.id, libraryFileUrl(workspace.id, a, "main"))
                }
                onOpen={() => setSearch({ asset: a.id })}
              />
            </li>
          ))}
        </ul>
        <LoadMore query={list} />
      </>
    )

  const hasLibrary =
    q !== "" || tab !== "all" || archived || !!list.data?.length

  return (
    <div className="relative" {...drop.handlers}>
      <PageHeader
        title={m.library.title}
        description={m.library.description}
        actions={hasLibrary ? uploadButton : null}
      />
      {canEdit ? (
        <input
          ref={input}
          type="file"
          multiple
          hidden
          accept={ACCEPTED_EXTENSIONS.join(",")}
          onChange={(e) => {
            if (e.target.files) uploads.add(e.target.files)
            e.target.value = ""
          }}
        />
      ) : (
        <ReadOnlyNote>{m.library.readOnlyHint}</ReadOnlyNote>
      )}
      <UploadQueue
        items={uploads.items}
        onCancel={uploads.cancel}
        onDismiss={uploads.dismiss}
      />
      {hasLibrary ? (
        <div className="mb-5 flex flex-wrap items-center gap-x-4 gap-y-3">
          <Tabs
            value={tab}
            onValueChange={(next) =>
              setSearch({
                kind:
                  next === "all" ? undefined : (next as LibrarySearch["kind"]),
              })
            }
          >
            <TabsList aria-label={m.library.kinds}>
              {LIBRARY_TABS.map((t) => (
                <TabsTrigger key={t} value={t} className="flex-none">
                  {m.library.tabs[t]}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <SearchField
            className="min-w-56 flex-1"
            value={q}
            onChange={(next) => setSearch({ q: next || undefined })}
            label={m.library.search}
            placeholder={m.library.searchPlaceholder}
          />
          <div className="flex items-center gap-2">
            <Switch
              id="library-archived"
              checked={archived}
              onCheckedChange={(on) =>
                setSearch({ archived: on ? true : undefined })
              }
            />
            <Label htmlFor="library-archived" className="text-sm font-normal">
              {m.library.showArchived}
            </Label>
          </div>
        </div>
      ) : null}
      {body}
      {drop.over ? <DropOverlay>{m.library.dropTitle}</DropOverlay> : null}
      <AssetSheet
        workspaceId={workspace.id}
        timezone={workspace.timezone}
        assetId={search.asset}
        canEdit={canEdit}
        onClose={() => setSearch({ asset: undefined })}
      />
    </div>
  )
}

/** One audio element for the page, so starting a track stops the last. */
function useAudioPlayer() {
  const audio = useRef<HTMLAudioElement | null>(null)
  const [playing, setPlaying] = useState<string>()
  const toggle = (id: string, src: string) => {
    if (!audio.current) {
      audio.current = new Audio()
      audio.current.addEventListener("ended", () => setPlaying(undefined))
      audio.current.addEventListener("pause", () => setPlaying(undefined))
    }
    const el = audio.current
    if (playing === id) {
      el.pause()
      return
    }
    el.src = src
    void el.play().then(
      () => setPlaying(id),
      () => setPlaying(undefined)
    )
  }
  return { playing, toggle }
}

function AssetCard({
  workspaceId,
  asset: a,
  playing,
  onTogglePlay,
  onOpen,
}: {
  workspaceId: string
  asset: LibraryAsset
  playing: boolean
  onTogglePlay: () => void
  onOpen: () => void
}) {
  const [hover, setHover] = useState(false)
  const Icon = kindIcon[a.kind]
  const ready = a.status === "ready"
  const meta = [
    m.library.kind[a.kind],
    a.durationSeconds
      ? formatDuration(Math.round(a.durationSeconds))
      : undefined,
    a.orientation ? m.library.orientation[a.orientation] : a.mood || undefined,
    m.library.origin[a.origin],
    a.usageCount > 0 ? m.library.uses(a.usageCount) : undefined,
  ].filter(Boolean)

  return (
    <div
      className={cn(
        "group/asset relative flex h-full flex-col overflow-hidden rounded-xl bg-card text-card-foreground shadow-xs ring-1 ring-foreground/10 transition-shadow hover:shadow-md hover:ring-foreground/20",
        a.archivedAt && "opacity-70"
      )}
      onPointerEnter={() => setHover(true)}
      onPointerLeave={() => setHover(false)}
    >
      <div className="relative aspect-[4/3] overflow-hidden bg-muted">
        {ready && a.hasPoster ? (
          hover && a.kind === "video" ? (
            <video
              src={libraryFileUrl(workspaceId, a, "main")}
              poster={libraryFileUrl(workspaceId, a, "poster")}
              className="size-full object-contain"
              muted
              loop
              autoPlay
              playsInline
            />
          ) : (
            <img
              src={libraryFileUrl(workspaceId, a, "poster")}
              alt=""
              loading="lazy"
              className="size-full object-contain"
            />
          )
        ) : (
          <div className="flex size-full items-center justify-center text-muted-foreground">
            {a.status === "processing" ? (
              <Loader2Icon className="size-6 animate-spin" />
            ) : a.status === "failed" ? (
              <AlertCircleIcon className="size-6 text-destructive" />
            ) : (
              <Icon className="size-8" />
            )}
          </div>
        )}
        {a.kind === "video" && ready ? (
          <span className="tabular absolute right-2 bottom-2 rounded-md bg-black/70 px-1.5 py-0.5 font-mono text-xs text-white">
            {formatDuration(Math.round(a.durationSeconds ?? 0))}
          </span>
        ) : null}
      </div>
      <div className="flex flex-1 items-start gap-3 p-4">
        <div className="min-w-0 flex-1 space-y-1">
          {/* The title opens the sheet; the whole card is its hit area. */}
          <button
            type="button"
            onClick={onOpen}
            aria-label={m.library.open(a.title)}
            className="block max-w-full truncate text-left font-medium outline-none group-hover/asset:underline group-hover/asset:underline-offset-4 after:absolute after:inset-0 after:rounded-xl focus-visible:after:ring-[3px] focus-visible:after:ring-ring"
          >
            {a.title}
          </button>
          <p className="truncate text-xs text-muted-foreground">
            {meta.join(" · ")}
          </p>
          {a.status !== "ready" || a.archivedAt ? (
            <div className="flex flex-wrap gap-1.5 pt-1">
              {a.status !== "ready" ? (
                <Badge
                  variant="secondary"
                  className={
                    a.status === "failed"
                      ? "bg-destructive/10 text-destructive"
                      : "bg-info/15 text-info"
                  }
                >
                  {m.library.status[a.status]}
                </Badge>
              ) : null}
              {a.archivedAt ? (
                <Badge variant="outline">{m.library.archivedBadge}</Badge>
              ) : null}
            </div>
          ) : null}
        </div>
        {a.kind === "music" && ready ? (
          <Button
            variant="outline"
            size="icon"
            className="relative z-10 shrink-0 rounded-full"
            aria-label={
              playing ? m.library.pause(a.title) : m.library.play(a.title)
            }
            aria-pressed={playing}
            onClick={onTogglePlay}
          >
            {playing ? <PauseIcon /> : <PlayIcon />}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function GridSkeleton() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
      {Array.from({ length: 8 }, (_, i) => (
        <div
          key={i}
          className="overflow-hidden rounded-xl ring-1 ring-foreground/10"
        >
          <Skeleton className="aspect-[4/3] rounded-none" />
          <div className="space-y-2 p-4">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  )
}
