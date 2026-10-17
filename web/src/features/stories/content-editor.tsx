import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { useBlocker } from "@tanstack/react-router"
import { InfoIcon, RotateCwIcon } from "lucide-react"
import { toast } from "sonner"

import {
  isApiError,
  type Deliverable,
  type DeliverableContent,
  type DeliverableEdit,
  type Story,
  type VideoMetadataContent,
} from "@/api/client"
import { ChipInput } from "@/components/chip-input"
import { Banner, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import {
  deliverableContentQuery,
  useEditDeliverableContent,
} from "@/queries/stories"
import {
  FIELD_KEYS,
  articleErrors,
  buildArticle,
  countChars,
  countWords,
  normalizeNewlines,
  parseArticle,
  type ArticleFields,
} from "./frontmatter"
import { DiscardChangesDialog } from "./discard-dialog"
import { Markdown } from "./markdown"
import { formatTimestamp } from "./model"

type EditorProps = {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  /** Leaves the editor (after saving, or once the reviewer agreed to discard). */
  onClose: () => void
  /** Reports whether there are unsaved changes, so the page can guard leaving them. */
  onDirtyChange?: (dirty: boolean) => void
}

/**
 * Edits a deliverable in review in place: an article's frontmatter and
 * body, or a video's narration and metadata. Replaces the review area of
 * the deliverable panel while open.
 */
export function ContentEditor(props: EditorProps) {
  const { workspaceId, story, deliverable: d, onClose } = props
  const query = useQuery(deliverableContentQuery(workspaceId, story.id, d.id))
  const title =
    d.kind === "video"
      ? m.editor.titleVideo(d.revision)
      : m.editor.titleArticle(d.revision)

  if (query.isPending) {
    return (
      <Shell title={title}>
        <div
          className="grid gap-4"
          aria-busy="true"
          aria-label={m.common.loading}
        >
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-[40vh] w-full" />
        </div>
      </Shell>
    )
  }
  if (query.isError) {
    return (
      <Shell title={title}>
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        <div className="flex justify-end">
          <Button variant="outline" onClick={onClose}>
            {m.common.cancel}
          </Button>
        </div>
      </Shell>
    )
  }
  // A reload (after a conflict) starts over from the newly loaded text.
  const key = query.dataUpdatedAt
  const reload = () => void query.refetch()
  // Name the revision being edited, which may lag the story after a conflict.
  const rev = query.data.revision
  return d.kind === "video" ? (
    <VideoEditor
      key={key}
      {...props}
      title={m.editor.titleVideo(rev)}
      content={query.data}
      onReload={reload}
    />
  ) : (
    <ArticleEditor
      key={key}
      {...props}
      title={m.editor.titleArticle(rev)}
      content={query.data}
      onReload={reload}
    />
  )
}

type InnerProps = EditorProps & {
  title: string
  content: DeliverableContent
  onReload: () => void
}

// ---- Shared pieces ----

function Shell({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section
      className="grid gap-6 rounded-xl border p-4 md:p-6"
      aria-label={title}
    >
      <header className="space-y-1">
        <h3 className="font-heading text-lg font-semibold tracking-tight">
          {title}
        </h3>
        {description ? (
          <p className="text-sm text-muted-foreground">{description}</p>
        ) : null}
      </header>
      {children}
    </section>
  )
}

/**
 * Asks before unsaved changes are lost: on Cancel, on navigating away
 * within the app and on closing or reloading the tab.
 */
function useUnsavedGuard(
  dirty: boolean,
  onDirtyChange: ((dirty: boolean) => void) | undefined
) {
  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    enableBeforeUnload: () => dirty,
    withResolver: true,
  })
  const report = useRef(onDirtyChange)
  useEffect(() => {
    report.current = onDirtyChange
  })
  useEffect(() => {
    report.current?.(dirty)
  }, [dirty])
  useEffect(() => () => report.current?.(false), [])
  return blocker
}

/** Server messages are lower-case fragments; show them as sentences. */
function sentence(s: string): string {
  const t = s.trim()
  if (!t) return t
  const first = t[0].toUpperCase() + t.slice(1)
  return /[.!?…)]$/.test(first) ? first : `${first}.`
}

/** What the server said about a failed save, shown above the buttons. */
function SaveError({
  error,
  onReload,
}: {
  error: unknown
  onReload: () => void
}) {
  if (!error) return null
  if (isApiError(error) && error.status === 409) {
    return (
      <Banner
        tone="warning"
        title={m.editor.conflictTitle}
        action={
          <Button size="sm" variant="outline" onClick={onReload}>
            <RotateCwIcon data-icon="inline-start" />
            {m.editor.reload}
          </Button>
        }
      >
        <p>{m.editor.conflictBody}</p>
      </Banner>
    )
  }
  if (isApiError(error, "ValidationFailed")) {
    return (
      <Banner title={m.editor.rejected}>
        <p>{sentence(error.message)}</p>
      </Banner>
    )
  }
  return <Banner title={errorMessage(error)} />
}

/** Cancel / Save, kept in view at the bottom while scrolling a long text. */
function Footer({
  dirty,
  saving,
  saveLabel,
  note,
  problems,
  error,
  onReload,
  onCancel,
  onSave,
}: {
  dirty: boolean
  saving: boolean
  saveLabel: string
  note?: ReactNode
  problems: boolean
  error: unknown
  onReload: () => void
  onCancel: () => void
  onSave: () => void
}) {
  return (
    <div className="sticky bottom-0 z-10 -mx-4 -mb-4 grid gap-3 rounded-b-xl border-t bg-background/95 px-4 py-3 backdrop-blur md:-mx-6 md:-mb-6 md:px-6">
      {note}
      <SaveError error={error} onReload={onReload} />
      <div className="flex flex-wrap items-center justify-end gap-3">
        {!dirty ? (
          <span className="text-sm text-muted-foreground">
            {m.editor.noChanges}
          </span>
        ) : problems ? (
          <span className="text-sm text-destructive">{m.editor.fixErrors}</span>
        ) : null}
        <Button type="button" variant="outline" onClick={onCancel}>
          {m.common.cancel}
        </Button>
        <Button
          type="button"
          disabled={!dirty || problems || saving}
          onClick={onSave}
        >
          {saving ? m.common.saving : saveLabel}
        </Button>
      </div>
    </div>
  )
}

function Counter({ text, bad }: { text: string; bad: boolean }) {
  return (
    <span
      className={cn(
        "tabular text-xs",
        bad ? "text-destructive" : "text-muted-foreground"
      )}
    >
      {text}
    </span>
  )
}

/** A labelled control with a counter beside the label and a hint or error under it. */
function EditField({
  id,
  label,
  counter,
  hint,
  error,
  children,
}: {
  id: string
  label: string
  counter?: ReactNode
  hint?: string
  error?: string
  children: ReactNode
}) {
  return (
    <Field data-invalid={!!error || undefined} className="gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <FieldLabel htmlFor={id}>{label}</FieldLabel>
        {counter}
      </div>
      {children}
      {error ? (
        <FieldError>{error}</FieldError>
      ) : hint ? (
        <FieldDescription>{hint}</FieldDescription>
      ) : null}
    </Field>
  )
}

/** Shared save flow: the mutation, the guard and the confirm dialogs. */
function useEditorState(props: InnerProps, dirty: boolean) {
  const { workspaceId, story, deliverable: d, onClose, onDirtyChange } = props
  const edit = useEditDeliverableContent(workspaceId, story.id, d.id)
  const blocker = useUnsavedGuard(dirty && !edit.isSuccess, onDirtyChange)
  const [confirmCancel, setConfirmCancel] = useState(false)

  const cancel = () => (dirty ? setConfirmCancel(true) : onClose())
  const save = async (body: DeliverableEdit, retime: boolean) => {
    let updated: Story
    try {
      updated = await edit.mutateAsync(body)
    } catch {
      return // shown above the buttons
    }
    const now = updated.deliverables.find((x) => x.id === d.id)
    toast.success(
      retime
        ? m.editor.savedRetime
        : m.editor.saved(now?.revision ?? d.revision + 1)
    )
    onClose()
  }

  const dialogs = (
    <>
      <DiscardChangesDialog
        open={confirmCancel}
        onKeep={() => setConfirmCancel(false)}
        onDiscard={() => {
          setConfirmCancel(false)
          onClose()
        }}
      />
      <DiscardChangesDialog
        open={blocker.status === "blocked"}
        onKeep={() => blocker.reset?.()}
        onDiscard={() => blocker.proceed?.()}
      />
    </>
  )
  return { edit, cancel, save, dialogs }
}

// ---- Article ----

const sameTags = (a: string[], b: string[]) =>
  a.length === b.length && a.every((t, i) => t === b[i])

function ArticleEditor(props: InnerProps) {
  const { title, content, deliverable: d, onReload } = props
  const doc = useMemo(
    () => parseArticle(content.article ?? ""),
    [content.article]
  )
  const [fields, setFields] = useState<ArticleFields>(doc.fields)
  const [body, setBody] = useState(doc.body)
  const set = <K extends keyof ArticleFields>(
    key: K,
    value: ArticleFields[K]
  ) => setFields((prev) => ({ ...prev, [key]: value }))

  const dirty =
    FIELD_KEYS.some((k) =>
      k === "tags"
        ? !sameTags(fields.tags, doc.fields.tags)
        : fields[k] !== doc.fields[k]
    ) || normalizeNewlines(body) !== doc.body
  const errors = articleErrors(fields, body)
  const problems = Object.keys(errors).length > 0
  const { edit, cancel, save, dialogs } = useEditorState(props, dirty)

  const words = countWords(body)
  const descChars = countChars(fields.description)
  const seoChars = countChars(fields.seoTitle)
  const otherKeys = doc.entries
    .map((e) => e.key)
    .filter((k) => !(FIELD_KEYS as readonly string[]).includes(k))
  const t = m.editor.article

  return (
    <Shell title={title} description={m.editor.descriptionArticle}>
      <div className="grid gap-5">
        <EditField id="edit-title" label={t.title} error={errors.title}>
          <Input
            id="edit-title"
            lang={d.language}
            value={fields.title}
            onChange={(e) => set("title", e.target.value)}
            aria-invalid={!!errors.title || undefined}
          />
        </EditField>
        <EditField
          id="edit-description"
          label={t.description}
          counter={
            <Counter
              text={m.editor.charsRange(descChars, 50, 200)}
              bad={descChars < 50 || descChars > 200}
            />
          }
          hint={t.descriptionHint}
          error={errors.description}
        >
          <Textarea
            id="edit-description"
            lang={d.language}
            rows={2}
            value={fields.description}
            onChange={(e) =>
              set("description", e.target.value.replace(/\n/g, " "))
            }
            aria-invalid={!!errors.description || undefined}
          />
        </EditField>
        <div className="grid gap-5 md:grid-cols-2">
          <EditField
            id="edit-slug"
            label={t.slug}
            hint={t.slugHint}
            error={errors.slug}
          >
            <Input
              id="edit-slug"
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
              value={fields.slug}
              onChange={(e) => set("slug", e.target.value.trim())}
              aria-invalid={!!errors.slug || undefined}
            />
          </EditField>
          <EditField
            id="edit-seo-title"
            label={t.seoTitle}
            counter={
              <Counter
                text={m.editor.chars(seoChars, 60)}
                bad={seoChars > 60}
              />
            }
            hint={t.seoTitleHint}
            error={errors.seoTitle}
          >
            <Input
              id="edit-seo-title"
              lang={d.language}
              value={fields.seoTitle}
              onChange={(e) => set("seoTitle", e.target.value)}
              aria-invalid={!!errors.seoTitle || undefined}
            />
          </EditField>
        </div>
        <EditField
          id="edit-tags"
          label={t.tags}
          hint={m.editor.tagsHint}
          error={errors.tags}
        >
          <ChipInput
            id="edit-tags"
            lang={d.language}
            value={fields.tags}
            onChange={(tags) => set("tags", tags)}
            placeholder={m.editor.tagsAdd}
            invalid={!!errors.tags}
          />
        </EditField>
        {otherKeys.length > 0 ? (
          <p className="text-xs text-muted-foreground">
            {t.otherKeys(otherKeys.join(", "))}
          </p>
        ) : null}

        <Tabs defaultValue="write" className="gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <span className="text-sm font-medium">{t.body}</span>
              <TabsList aria-label={t.body}>
                <TabsTrigger value="write">{t.write}</TabsTrigger>
                <TabsTrigger value="preview">{t.preview}</TabsTrigger>
              </TabsList>
            </div>
            <Counter
              text={
                d.article
                  ? m.editor.wordsTarget(words, d.article.words)
                  : m.editor.words(words)
              }
              bad={words < 100}
            />
          </div>
          <TabsContent value="write" className="grid gap-2">
            <Textarea
              aria-label={t.body}
              lang={d.language}
              spellCheck
              className="min-h-[60vh] font-mono text-[13px] leading-relaxed md:text-[13px]"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              aria-invalid={!!errors.body || undefined}
            />
            {errors.body ? (
              <p className="text-sm text-destructive">{errors.body}</p>
            ) : null}
          </TabsContent>
          <TabsContent value="preview">
            <div className="min-h-[60vh] rounded-xl border p-5 md:p-8">
              {fields.title ? (
                <h1
                  lang={d.language}
                  className="mb-6 font-heading text-2xl leading-tight font-semibold tracking-tight"
                >
                  {fields.title}
                </h1>
              ) : null}
              {body.trim() ? (
                <Markdown
                  source={body}
                  lang={d.language}
                  className="max-w-[72ch] text-[15px]"
                />
              ) : (
                <p className="text-sm text-muted-foreground">
                  {t.emptyPreview}
                </p>
              )}
            </div>
          </TabsContent>
        </Tabs>
      </div>
      <Footer
        dirty={dirty}
        saving={edit.isPending}
        saveLabel={m.editor.save}
        problems={dirty && problems}
        error={edit.error}
        onReload={onReload}
        onCancel={cancel}
        onSave={() =>
          void save(
            {
              revision: content.revision,
              article: buildArticle(doc, fields, body),
            },
            false
          )
        }
      />
      {dialogs}
    </Shell>
  )
}

// ---- Video ----

/** Narration pace used for the estimate: about two words a second. */
const WORDS_PER_SECOND = 2
/** How far off the target the estimate may be before it is "clearly" off. */
const TOLERANCE = 0.25
/** The server's limit on all tags joined with commas. */
const MAX_TAG_CHARS = 500
const MAX_SHORT_SECONDS = 180

function metadataErrors(meta: VideoMetadataContent) {
  const t = m.editor.video
  const errors: { title?: string; description?: string; tags?: string } = {}
  if (!meta.title.trim()) errors.title = t.titleRequired
  else if (countChars(meta.title) > 100) errors.title = t.titleTooLong
  else if (/[<>]/.test(meta.title)) errors.title = t.angleBrackets
  if (countChars(meta.description) > 5000)
    errors.description = t.descriptionTooLong
  else if (/[<>]/.test(meta.description)) errors.description = t.angleBrackets
  const tagChars = countChars(meta.tags.join(","))
  if (tagChars > MAX_TAG_CHARS) errors.tags = t.tagsTooLong(tagChars)
  return errors
}

const sameMeta = (a: VideoMetadataContent, b: VideoMetadataContent) =>
  a.title === b.title &&
  a.description === b.description &&
  sameTags(a.tags, b.tags)

function VideoEditor(props: InnerProps) {
  const { title, content, deliverable: d, onReload } = props
  const original = content.narration
  const [narration, setNarration] = useState(original ?? "")
  const [meta, setMeta] = useState<VideoMetadataContent | undefined>(
    content.metadata
  )
  const t = m.editor.video

  // The server compares the narration ignoring surrounding whitespace.
  const narrationChanged =
    original !== undefined &&
    normalizeNewlines(narration).trim() !== normalizeNewlines(original).trim()
  const metaChanged =
    !!meta && !!content.metadata && !sameMeta(meta, content.metadata)
  const dirty = narrationChanged || metaChanged
  const narrationError =
    narrationChanged && !narration.trim() ? t.narrationRequired : undefined
  const metaErrors = meta ? metadataErrors(meta) : {}
  const problems =
    !!narrationError || (metaChanged && Object.keys(metaErrors).length > 0)
  const { edit, cancel, save, dialogs } = useEditorState(props, dirty)
  // A script waiting for approval is not narrated yet: nothing to re-time.
  const retime = narrationChanged && d.stage !== "script"

  const words = countWords(narration)
  const estimate = Math.round(words / WORDS_PER_SECOND)
  const target = d.video?.durationSeconds
  let pace: string | undefined
  if (target && words > 0) {
    if (estimate > target * (1 + TOLERANCE))
      pace = t.tooLong(formatTimestamp(estimate), formatTimestamp(target))
    else if (estimate < target * (1 - TOLERANCE))
      pace = t.tooShort(formatTimestamp(estimate), formatTimestamp(target))
  }
  const overShort = d.video?.format === "short" && estimate > MAX_SHORT_SECONDS

  const setMetaField = <K extends keyof VideoMetadataContent>(
    key: K,
    value: VideoMetadataContent[K]
  ) => setMeta((prev) => (prev ? { ...prev, [key]: value } : prev))

  const submit = () => {
    const body: DeliverableEdit = { revision: content.revision }
    if (narrationChanged) body.narration = normalizeNewlines(narration)
    if (metaChanged && meta)
      body.metadata = {
        ...meta,
        title: meta.title.trim(),
        description: meta.description.trim(),
      }
    void save(body, retime)
  }

  const titleChars = meta ? countChars(meta.title) : 0
  const descChars = meta ? countChars(meta.description) : 0

  return (
    <Shell title={title} description={m.editor.descriptionVideo}>
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
        <div className="grid gap-3">
          {original === undefined ? (
            <p className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t.noNarration}
            </p>
          ) : (
            <EditField
              id="edit-narration"
              label={t.narration}
              counter={
                <span className="tabular text-xs text-muted-foreground">
                  {m.editor.words(words)} ·{" "}
                  {t.estimate(formatTimestamp(estimate))}
                  {target ? ` · ${t.target(formatTimestamp(target))}` : ""}
                </span>
              }
              hint={t.narrationHint}
              error={narrationError}
            >
              <Textarea
                id="edit-narration"
                lang={d.language}
                spellCheck
                className="min-h-[50vh] text-[15px] leading-relaxed md:text-[15px]"
                value={narration}
                onChange={(e) => setNarration(e.target.value)}
                aria-invalid={!!narrationError || undefined}
              />
            </EditField>
          )}
          {pace || overShort ? (
            <Banner
              tone="warning"
              title={overShort ? t.overShort : (pace ?? "")}
            >
              {overShort && pace ? <p>{pace}</p> : null}
            </Banner>
          ) : null}
        </div>

        <div className="grid gap-5 rounded-xl bg-muted/30 p-4">
          <h4 className="text-sm font-medium">
            {t.metadata}
          </h4>
          {!meta ? (
            <p className="text-sm text-muted-foreground">{t.noMetadata}</p>
          ) : (
            <>
              <EditField
                id="edit-video-title"
                label={t.title}
                counter={
                  <Counter
                    text={m.editor.chars(titleChars, 100)}
                    bad={titleChars > 100}
                  />
                }
                error={metaErrors.title}
              >
                <Input
                  id="edit-video-title"
                  lang={d.language}
                  value={meta.title}
                  onChange={(e) => setMetaField("title", e.target.value)}
                  aria-invalid={!!metaErrors.title || undefined}
                />
              </EditField>
              <EditField
                id="edit-video-description"
                label={t.description}
                counter={
                  <Counter
                    text={m.editor.chars(descChars, 5000)}
                    bad={descChars > 5000}
                  />
                }
                error={metaErrors.description}
              >
                <Textarea
                  id="edit-video-description"
                  lang={d.language}
                  rows={8}
                  className="max-h-[50vh] overflow-y-auto"
                  value={meta.description}
                  onChange={(e) => setMetaField("description", e.target.value)}
                  aria-invalid={!!metaErrors.description || undefined}
                />
              </EditField>
              <EditField
                id="edit-video-tags"
                label={t.tags}
                hint={m.editor.tagsHint}
                error={metaErrors.tags}
              >
                <ChipInput
                  id="edit-video-tags"
                  lang={d.language}
                  value={meta.tags}
                  onChange={(tags) => setMetaField("tags", tags)}
                  placeholder={m.editor.tagsAdd}
                  invalid={!!metaErrors.tags}
                />
              </EditField>
            </>
          )}
        </div>
      </div>
      <Footer
        dirty={dirty}
        saving={edit.isPending}
        saveLabel={retime ? m.editor.saveRetime : m.editor.save}
        note={
          retime ? (
            <p className="flex gap-2 rounded-lg border border-info/30 bg-info/5 px-3 py-2 text-sm">
              <InfoIcon className="mt-0.5 size-4 shrink-0 text-info" />
              <span>{m.editor.retimeNote}</span>
            </p>
          ) : null
        }
        problems={dirty && problems}
        error={edit.error}
        onReload={onReload}
        onCancel={cancel}
        onSave={submit}
      />
      {dialogs}
    </Shell>
  )
}
