import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ArchiveIcon,
  ArchiveRestoreIcon,
  Loader2Icon,
  RotateCwIcon,
  Trash2Icon,
} from "lucide-react"
import { toast } from "sonner"

import type { LibraryAsset, LibraryAssetDetails } from "@/api/client"
import { ChipInput } from "@/components/chip-input"
import { Banner } from "@/components/page"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { useMediaQuery } from "@/hooks/use-media-query"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatBytes } from "@/lib/format"
import { formatDateTime, formatDuration } from "@/lib/time"
import {
  libraryAssetQuery,
  libraryFileUrl,
  useDeleteLibraryAsset,
  useRetryLibraryAsset,
  useUpdateLibraryAsset,
} from "@/queries/library"

/** Server limits (domain library). */
const MAX_TITLE = 200
const MAX_DESCRIPTION = 2000
const MAX_TAGS = 20

/** The asset named in the page's search, in a sheet: preview, details and actions. */
export function AssetSheet({
  workspaceId,
  timezone,
  assetId,
  canEdit,
  onClose,
}: {
  workspaceId: string
  timezone: string
  assetId?: string
  canEdit: boolean
  onClose: () => void
}) {
  const phone = useMediaQuery("(max-width: 639px)")
  const asset = useQuery({
    ...libraryAssetQuery(workspaceId, assetId ?? ""),
    enabled: !!assetId,
  })
  const a = asset.data
  return (
    <Sheet open={!!assetId} onOpenChange={(open) => !open && onClose()}>
      <SheetContent
        side={phone ? "bottom" : "right"}
        className="gap-0 overflow-y-auto data-[side=bottom]:max-h-[88svh] data-[side=right]:w-full data-[side=right]:sm:max-w-lg"
      >
        <SheetHeader className="pr-12">
          <SheetTitle className="font-semibold break-words">
            {a?.title ??
              (asset.isError ? (
                m.library.title
              ) : (
                <Skeleton className="h-5 w-48" />
              ))}
          </SheetTitle>
          <SheetDescription>
            {a
              ? m.library.sheet.description(
                  m.library.kind[a.kind],
                  m.library.origin[a.origin]
                )
              : null}
          </SheetDescription>
        </SheetHeader>
        {asset.isError ? (
          <div className="px-4 pb-4">
            <Banner title={m.library.sheet.notFound}>
              {errorMessage(asset.error)}
            </Banner>
          </div>
        ) : a ? (
          // Keyed by the asset and its version, so the form starts from what
          // the server holds after every save.
          <SheetBody
            key={`${a.id}-${a.updatedAt}`}
            workspaceId={workspaceId}
            timezone={timezone}
            asset={a}
            canEdit={canEdit}
            onDeleted={onClose}
          />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function SheetBody({
  workspaceId,
  timezone,
  asset: a,
  canEdit,
  onDeleted,
}: {
  workspaceId: string
  timezone: string
  asset: LibraryAsset
  canEdit: boolean
  onDeleted: () => void
}) {
  const update = useUpdateLibraryAsset(workspaceId)
  const retry = useRetryLibraryAsset(workspaceId)
  const remove = useDeleteLibraryAsset(workspaceId)
  const [deleting, setDeleting] = useState(false)

  const setArchived = async (archived: boolean) => {
    try {
      await update.mutateAsync({ assetId: a.id, archived })
      toast.success(
        archived
          ? m.library.sheet.archived(a.title)
          : m.library.sheet.unarchived(a.title)
      )
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <div className="grid gap-5 px-4 pb-6">
      <Preview workspaceId={workspaceId} asset={a} />
      {a.status === "processing" ? (
        <div className="flex gap-2.5 rounded-lg border border-info/30 bg-info/5 px-4 py-3 text-sm">
          <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin text-info" />
          <div className="space-y-0.5">
            <p className="font-medium">{m.library.sheet.processingTitle}</p>
            <p className="text-muted-foreground">
              {m.library.sheet.processingBody}
            </p>
          </div>
        </div>
      ) : null}
      {a.status === "failed" ? (
        <Banner
          title={m.library.sheet.failedTitle}
          action={
            canEdit ? (
              <Button
                variant="outline"
                size="sm"
                disabled={retry.isPending}
                onClick={() =>
                  retry.mutate(a.id, {
                    onSuccess: () =>
                      toast.success(m.library.sheet.retried(a.title)),
                    onError: (err) => toast.error(errorMessage(err)),
                  })
                }
              >
                <RotateCwIcon data-icon="inline-start" />
                {m.library.sheet.retry}
              </Button>
            ) : null
          }
        >
          <p>{m.library.sheet.failedBody}</p>
          {a.failure ? (
            <details className="mt-2">
              <summary className="cursor-pointer text-xs">
                {m.library.sheet.technical}
              </summary>
              <p className="mt-1 font-mono text-xs break-words">{a.failure}</p>
            </details>
          ) : null}
        </Banner>
      ) : null}
      <Facts asset={a} timezone={timezone} />
      <Separator />
      <DetailsForm workspaceId={workspaceId} asset={a} canEdit={canEdit} />
      {canEdit ? (
        <>
          <Separator />
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex flex-col gap-1">
              <Button
                variant="outline"
                className="w-fit"
                disabled={update.isPending}
                onClick={() => void setArchived(!a.archivedAt)}
              >
                {a.archivedAt ? (
                  <ArchiveRestoreIcon data-icon="inline-start" />
                ) : (
                  <ArchiveIcon data-icon="inline-start" />
                )}
                {a.archivedAt
                  ? m.library.sheet.unarchive
                  : m.library.sheet.archive}
              </Button>
              {a.archivedAt ? null : (
                <p className="max-w-xs text-xs text-muted-foreground">
                  {m.library.sheet.archiveHint}
                </p>
              )}
            </div>
            <Button variant="destructive" onClick={() => setDeleting(true)}>
              <Trash2Icon data-icon="inline-start" />
              {m.library.sheet.delete}
            </Button>
          </div>
          <AlertDialog open={deleting} onOpenChange={setDeleting}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {m.library.sheet.deleteTitle}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {m.library.sheet.deleteBody(a.title)}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  disabled={remove.isPending}
                  onClick={() =>
                    remove.mutate(a.id, {
                      onSuccess: () => {
                        toast.success(m.library.sheet.deleted(a.title))
                        setDeleting(false)
                        onDeleted()
                      },
                      onError: (err) => toast.error(errorMessage(err)),
                    })
                  }
                >
                  {m.library.sheet.delete}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </>
      ) : null}
    </div>
  )
}

function Preview({
  workspaceId,
  asset: a,
}: {
  workspaceId: string
  asset: LibraryAsset
}) {
  if (a.status !== "ready") return null
  const main = libraryFileUrl(workspaceId, a, "main")
  if (a.kind === "music")
    return (
      // oxlint-disable-next-line jsx-a11y/media-has-caption -- instrumental music has no words to caption
      <audio controls preload="metadata" src={main} className="w-full" />
    )
  return (
    <div className="overflow-hidden rounded-lg bg-muted">
      {a.kind === "video" ? (
        // oxlint-disable-next-line jsx-a11y/media-has-caption -- stock footage is silent
        <video
          controls
          muted
          loop
          playsInline
          preload="metadata"
          src={main}
          poster={
            a.hasPoster ? libraryFileUrl(workspaceId, a, "poster") : undefined
          }
          className="max-h-[50svh] w-full object-contain"
        />
      ) : (
        <img
          src={main}
          alt={a.description || a.title}
          className="max-h-[50svh] w-full object-contain"
        />
      )}
    </div>
  )
}

function Facts({
  asset: a,
  timezone,
}: {
  asset: LibraryAsset
  timezone: string
}) {
  const f = m.library.sheet.facts
  const rows: Array<[string, string]> = []
  if (a.filename) rows.push([f.filename, a.filename])
  rows.push([f.size, formatBytes(a.size)])
  if (a.width && a.height)
    rows.push([
      f.dimensions,
      `${a.width} × ${a.height}${a.orientation ? ` · ${m.library.orientation[a.orientation]}` : ""}`,
    ])
  if (a.durationSeconds)
    rows.push([f.duration, formatDuration(Math.round(a.durationSeconds))])
  rows.push([f.added, formatDateTime(a.createdAt, timezone)])
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="tabular min-w-0 break-words">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

type Draft = Required<Omit<LibraryAssetDetails, "credit">> & {
  author: string
  url: string
  licence: string
}

const draftOf = (a: LibraryAsset): Draft => ({
  title: a.title,
  description: a.description,
  tags: a.tags,
  mood: a.mood ?? "",
  tempo: a.tempo ?? 0,
  author: a.credit.author,
  url: a.credit.url,
  licence: a.credit.licence,
})

function problemsOf(d: Draft) {
  const s = m.library.sheet
  const out: Partial<Record<"title" | "tempo" | "url", string>> = {}
  if (!d.title.trim()) out.title = s.titleRequired
  else if (d.title.trim().length > MAX_TITLE) out.title = s.titleTooLong
  if (!Number.isInteger(d.tempo) || d.tempo < 0 || d.tempo > 300)
    out.tempo = s.tempoInvalid
  if (d.url.trim() && !/^https?:\/\/[^\s/]+/i.test(d.url.trim()))
    out.url = s.urlInvalid
  return out
}

function DetailsForm({
  workspaceId,
  asset: a,
  canEdit,
}: {
  workspaceId: string
  asset: LibraryAsset
  canEdit: boolean
}) {
  const s = m.library.sheet
  const update = useUpdateLibraryAsset(workspaceId)
  const [draft, setDraft] = useState(() => draftOf(a))
  const [touched, setTouched] = useState(false)
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) =>
    setDraft((d) => ({ ...d, [k]: v }))
  const problems = problemsOf(draft)
  const dirty = JSON.stringify(draft) !== JSON.stringify(draftOf(a))
  const music = a.kind === "music"

  const save = async () => {
    setTouched(true)
    if (Object.keys(problems).length > 0) return
    try {
      await update.mutateAsync({
        assetId: a.id,
        details: {
          title: draft.title.trim(),
          description: draft.description.trim(),
          tags: draft.tags,
          mood: music ? draft.mood.trim() : undefined,
          tempo: music ? draft.tempo : undefined,
          credit: {
            author: draft.author.trim(),
            url: draft.url.trim(),
            licence: draft.licence.trim(),
          },
        },
      })
      toast.success(s.saved)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        void save()
      }}
    >
      <h3 className="font-heading font-medium">{s.details}</h3>
      <Field data-invalid={touched && !!problems.title}>
        <FieldLabel htmlFor="asset-title">{s.titleLabel}</FieldLabel>
        <Input
          id="asset-title"
          value={draft.title}
          maxLength={MAX_TITLE}
          disabled={!canEdit}
          aria-invalid={touched && !!problems.title}
          onChange={(e) => set("title", e.target.value)}
        />
        {touched && problems.title ? (
          <FieldError>{problems.title}</FieldError>
        ) : null}
      </Field>
      <Field>
        <FieldLabel htmlFor="asset-description">
          {s.descriptionLabel}
        </FieldLabel>
        <Textarea
          id="asset-description"
          value={draft.description}
          maxLength={MAX_DESCRIPTION}
          rows={3}
          disabled={!canEdit}
          onChange={(e) => set("description", e.target.value)}
        />
        <FieldDescription>{s.descriptionHint}</FieldDescription>
      </Field>
      <Field>
        <FieldLabel htmlFor="asset-tags">{s.tagsLabel}</FieldLabel>
        <ChipInput
          id="asset-tags"
          value={draft.tags}
          max={MAX_TAGS}
          normalize={(t) => t.trim().toLowerCase()}
          disabled={!canEdit}
          placeholder={s.tagsAdd}
          onChange={(tags) => set("tags", tags)}
        />
      </Field>
      {music ? (
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field>
            <FieldLabel htmlFor="asset-mood">{s.moodLabel}</FieldLabel>
            <Input
              id="asset-mood"
              value={draft.mood}
              maxLength={100}
              placeholder={s.moodPlaceholder}
              disabled={!canEdit}
              onChange={(e) => set("mood", e.target.value)}
            />
          </Field>
          <Field data-invalid={touched && !!problems.tempo}>
            <FieldLabel htmlFor="asset-tempo">{s.tempoLabel}</FieldLabel>
            <Input
              id="asset-tempo"
              type="number"
              inputMode="numeric"
              min={0}
              max={300}
              value={draft.tempo || ""}
              disabled={!canEdit}
              aria-invalid={touched && !!problems.tempo}
              onChange={(e) =>
                set("tempo", e.target.value === "" ? 0 : Number(e.target.value))
              }
            />
            {touched && problems.tempo ? (
              <FieldError>{problems.tempo}</FieldError>
            ) : null}
          </Field>
        </div>
      ) : null}
      <fieldset className="grid gap-3">
        <legend className="mb-1 text-sm font-medium">{s.creditTitle}</legend>
        <p className="-mt-1 text-xs text-muted-foreground">{s.creditHint}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="asset-author">{s.authorLabel}</FieldLabel>
            <Input
              id="asset-author"
              value={draft.author}
              maxLength={300}
              disabled={!canEdit}
              onChange={(e) => set("author", e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="asset-licence">{s.licenceLabel}</FieldLabel>
            <Input
              id="asset-licence"
              value={draft.licence}
              maxLength={300}
              disabled={!canEdit}
              onChange={(e) => set("licence", e.target.value)}
            />
          </Field>
        </div>
        <Field data-invalid={touched && !!problems.url}>
          <FieldLabel htmlFor="asset-url">{s.urlLabel}</FieldLabel>
          <Input
            id="asset-url"
            type="url"
            inputMode="url"
            value={draft.url}
            maxLength={300}
            placeholder="https://"
            disabled={!canEdit}
            aria-invalid={touched && !!problems.url}
            onChange={(e) => set("url", e.target.value)}
          />
          {touched && problems.url ? (
            <FieldError>{problems.url}</FieldError>
          ) : null}
        </Field>
      </fieldset>
      {canEdit ? (
        <Button
          type="submit"
          variant={dirty ? "default" : "outline"}
          className="w-fit"
          disabled={update.isPending || !dirty}
        >
          {update.isPending ? (
            <Loader2Icon data-icon="inline-start" className="animate-spin" />
          ) : null}
          {s.save}
        </Button>
      ) : null}
    </form>
  )
}
