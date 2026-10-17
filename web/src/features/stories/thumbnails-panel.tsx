import { useId, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { DownloadIcon, MessageSquareIcon, PencilIcon } from "lucide-react"

import type { Deliverable, Revision, Thumbnail } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { integrationsQuery } from "@/queries/integrations"
import {
  artifactTextQuery,
  artifactUrl,
  useEditThumbnail,
  usePickThumbnail,
} from "@/queries/stories"
import type { ArtifactRef } from "./previews"

/** Statuses in which the variant a video goes out with can still change. */
const PICKABLE: Deliverable["status"][] = ["review", "finalizing", "ready"]

/** The longest thumbnail text the server takes, in characters and words. */
const MAX_CHARS = 60
const MAX_WORDS = 5

const thumbName = (n: number) => `thumb-${n}.jpg`

/**
 * A long video's thumbnail variants at their real 16:9 shape, each also at
 * the size a phone's feed shows it beside the title. On the latest
 * revision an editor picks the one the video goes out with and rewords
 * them; once it is live they are there to download.
 */
export function ThumbnailsPanel({
  deliverable: d,
  revision: r,
  artifacts,
  canEdit,
  onNote,
}: {
  deliverable: Deliverable
  revision: Revision
  artifacts: ArtifactRef
  canEdit: boolean
  /** Opens the feedback form on a note about the thumbnails; absent when notes cannot be sent. */
  onNote?: () => void
}) {
  const workspace = useWorkspace()
  const thumbs = r.thumbnails
  const pick = usePickThumbnail(artifacts.workspaceId, artifacts.storyId, d.id)
  const latest = r.number === d.revision
  const canPick = canEdit && latest && PICKABLE.includes(d.status)
  const canReword = canEdit && latest && d.status === "review"
  const metadata = useQuery(
    artifactTextQuery(
      artifacts.workspaceId,
      artifacts.storyId,
      d.id,
      r.number,
      "metadata.json"
    )
  )
  const integrations = useQuery(integrationsQuery(workspace.id))
  if (!thumbs) return null

  const chosen = thumbs.variants.some((v) => v.n === d.thumbnail)
    ? d.thumbnail!
    : thumbs.recommended
  const url = (name: string, download = false) =>
    artifactUrl(
      artifacts.workspaceId,
      artifacts.storyId,
      d.id,
      r.number,
      name,
      download
    )
  const feed = {
    title: titleOf(metadata.data),
    channel: integrations.data?.youtube.channelTitle || workspace.name,
  }
  const redrawing = latest && d.status === "rendering"
  const hint = !latest
    ? m.thumbnails.olderHint
    : redrawing
      ? m.thumbnails.redrawingHint
      : d.status === "published"
        ? m.thumbnails.publishedHint
        : canPick
          ? m.thumbnails.pickHint
          : m.thumbnails.readHint

  const cards = thumbs.variants.map((v) => (
    <VariantCard
      key={v.n}
      variant={v}
      deliverable={d}
      revision={r}
      artifacts={artifacts}
      src={url(thumbName(v.n))}
      download={url(thumbName(v.n), true)}
      recommended={v.n === thumbs.recommended}
      chosen={latest && v.n === chosen}
      uploaded={latest && v.n === chosen && d.status === "published"}
      pickable={canPick}
      rewordable={canReword}
      feed={feed}
    />
  ))

  return (
    <div className="grid gap-4" aria-busy={redrawing || undefined}>
      <p
        className="text-sm text-muted-foreground"
        role={redrawing ? "status" : undefined}
      >
        {hint}
      </p>
      {canPick ? (
        <RadioGroup
          aria-label={m.thumbnails.pickLabel}
          value={String(chosen)}
          disabled={pick.isPending}
          onValueChange={(v) => pick.mutate(Number(v))}
          className="gap-5"
        >
          {cards}
        </RadioGroup>
      ) : (
        <ul className="grid gap-5">
          {cards.map((c) => (
            <li key={c.key}>{c}</li>
          ))}
        </ul>
      )}
      {pick.error ? (
        <p className="text-sm text-destructive" role="alert">
          {errorMessage(pick.error)}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {onNote && latest ? (
          <Button type="button" size="sm" variant="outline" onClick={onNote}>
            <MessageSquareIcon data-icon="inline-start" />
            {m.thumbnails.note}
          </Button>
        ) : null}
        <a
          href={url("thumbnails.zip", true)}
          download="thumbnails.zip"
          className={buttonVariants({ size: "sm", variant: "ghost" })}
        >
          <DownloadIcon data-icon="inline-start" />
          {m.thumbnails.downloadAll}
        </a>
      </div>
    </div>
  )
}

function titleOf(raw: string | undefined): string {
  if (!raw) return ""
  try {
    const meta: unknown = JSON.parse(raw)
    return meta &&
      typeof meta === "object" &&
      "title" in meta &&
      typeof meta.title === "string"
      ? meta.title
      : ""
  } catch {
    return ""
  }
}

function VariantCard({
  variant: v,
  deliverable: d,
  revision: r,
  artifacts,
  src,
  download,
  recommended,
  chosen,
  uploaded,
  pickable,
  rewordable,
  feed,
}: {
  variant: Thumbnail
  deliverable: Deliverable
  revision: Revision
  artifacts: ArtifactRef
  src: string
  download: string
  recommended: boolean
  chosen: boolean
  uploaded: boolean
  pickable: boolean
  rewordable: boolean
  feed: { title: string; channel: string }
}) {
  const labelId = useId()
  const alt = m.thumbnails.alt(v.n, v.text)
  return (
    <div
      className={cn(
        "grid gap-3 rounded-xl border p-3",
        chosen &&
          "border-foreground/40 bg-muted/40 inset-ring inset-ring-foreground/20"
      )}
    >
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {pickable ? (
          <RadioGroupItem value={String(v.n)} aria-labelledby={labelId} />
        ) : null}
        <span id={labelId} className="font-medium">
          {m.thumbnails.variant(v.n)}
          <span className="font-normal text-muted-foreground">
            {" "}
            · {v.concept}
          </span>
        </span>
        {recommended ? (
          <Badge variant="secondary">{m.thumbnails.recommended}</Badge>
        ) : null}
        {uploaded ? (
          <Badge>{m.thumbnails.uploaded}</Badge>
        ) : chosen && pickable ? (
          <Badge variant="outline">{m.thumbnails.goesOut}</Badge>
        ) : null}
        <a
          href={download}
          download={`thumbnail-${v.n}.jpg`}
          className={cn(
            buttonVariants({ size: "icon-xs", variant: "ghost" }),
            "ml-auto text-muted-foreground"
          )}
          aria-label={m.thumbnails.download(v.n)}
          title={m.thumbnails.download(v.n)}
        >
          <DownloadIcon />
        </a>
      </div>
      <img
        src={src}
        alt={alt}
        loading="lazy"
        width={1280}
        height={720}
        className="aspect-video h-auto w-full rounded-lg border bg-muted object-cover"
      />
      <div className="flex gap-3" aria-hidden="true">
        <img
          src={src}
          alt=""
          loading="lazy"
          width={168}
          height={94}
          className="aspect-video h-[94px] w-[168px] shrink-0 rounded-md bg-muted object-cover"
        />
        <div className="min-w-0 text-[13px] leading-snug">
          <p lang={d.language} className="line-clamp-2 font-medium">
            {feed.title || m.thumbnails.untitled}
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">{feed.channel}</p>
        </div>
      </div>
      <Words
        variant={v}
        deliverable={d}
        revision={r}
        artifacts={artifacts}
        editable={rewordable}
      />
    </div>
  )
}

/** A variant's words, which an editor can change in place. */
function Words({
  variant: v,
  deliverable: d,
  revision: r,
  artifacts,
  editable,
}: {
  variant: Thumbnail
  deliverable: Deliverable
  revision: Revision
  artifacts: ArtifactRef
  editable: boolean
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const edit = useEditThumbnail(artifacts.workspaceId, artifacts.storyId, d.id)
  const fieldId = useId()
  const fieldRef = useRef<HTMLInputElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  const open = () => {
    setDraft(v.text)
    requestAnimationFrame(() => fieldRef.current?.select())
  }
  const close = () => {
    setDraft(null)
    edit.reset()
    requestAnimationFrame(() => triggerRef.current?.focus())
  }

  if (draft === null) {
    return (
      <div className="flex items-start gap-2">
        <p className="min-w-0 flex-1 text-sm">
          <span className="text-muted-foreground">{m.thumbnails.words}: </span>
          <span lang={d.language} className="font-medium">
            {v.text}
          </span>
        </p>
        {editable ? (
          <Button
            ref={triggerRef}
            type="button"
            size="icon-xs"
            variant="ghost"
            aria-label={m.thumbnails.edit(v.n)}
            title={m.thumbnails.edit(v.n)}
            onClick={open}
          >
            <PencilIcon />
          </Button>
        ) : null}
      </div>
    )
  }

  const words = draft.trim().split(/\s+/).filter(Boolean).length
  const problem =
    draft.trim() === ""
      ? m.thumbnails.emptyWords
      : words > MAX_WORDS
        ? m.thumbnails.tooManyWords(words, MAX_WORDS)
        : [...draft.trim()].length > MAX_CHARS
          ? m.thumbnails.tooLong(MAX_CHARS)
          : undefined
  const save = () => {
    if (problem) return
    edit.mutate(
      { n: v.n, revision: r.number, text: draft.trim() },
      { onSuccess: () => setDraft(null) }
    )
  }
  return (
    <form
      className="grid gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <label
        htmlFor={fieldId}
        className="text-xs font-medium text-muted-foreground"
      >
        {m.thumbnails.wordsLabel(v.n)}
      </label>
      <Input
        ref={fieldRef}
        id={fieldId}
        lang={d.language}
        value={draft}
        maxLength={MAX_CHARS}
        disabled={edit.isPending}
        aria-invalid={!!problem || !!edit.error || undefined}
        aria-describedby={`${fieldId}-hint`}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault()
            close()
          }
        }}
      />
      <p
        id={`${fieldId}-hint`}
        className={cn(
          "text-xs",
          problem || edit.error ? "text-destructive" : "text-muted-foreground"
        )}
      >
        {edit.error
          ? errorMessage(edit.error)
          : (problem ?? m.thumbnails.wordsHint(MAX_WORDS))}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="sm" disabled={!!problem || edit.isPending}>
          {m.thumbnails.save}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={close}
          disabled={edit.isPending}
        >
          {m.annotation.cancel}
        </Button>
      </div>
    </form>
  )
}
