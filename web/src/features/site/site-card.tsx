import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { SiteOverview, SiteSchedule } from "@/api/client"
import { Banner, ErrorState, ReadOnlyNote } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { toInt } from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { siteQuery, useSaveSite } from "@/queries/site"

const SCHEDULES: SiteSchedule[] = ["weekly", "daily", "off"]
const DEFAULT_MAX_PAGES = 500
const t = m.site.card

// The server's rules (site.ParseURL, Settings), checked here for quick feedback.
const urlError = (s: string) => {
  try {
    const u = new URL(s.trim())
    return (u.protocol === "http:" || u.protocol === "https:") &&
      u.host &&
      !u.search &&
      !u.hash
      ? undefined
      : t.urlInvalid
  } catch {
    return t.urlInvalid
  }
}
const maxPagesError = (s: string) => {
  const n = toInt(s)
  return n !== undefined && n >= 1 && n <= 5000 ? undefined : t.maxPagesInvalid
}

/** Settings › Site: the website Ekin crawls. Owners edit. */
export function SiteCard() {
  const workspace = useWorkspace()
  const query = useQuery(siteQuery(workspace.id))

  let body
  if (query.isPending) body = <Skeleton className="h-56 w-full rounded-xl" />
  else if (query.isError)
    body = (
      <ErrorState error={query.error} onRetry={() => void query.refetch()} />
    )
  // Re-mount the form when the stored settings change underneath it.
  else
    body = (
      <SiteForm
        key={JSON.stringify(query.data.site ?? null)}
        overview={query.data}
      />
    )

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t.title}</CardTitle>
        <CardDescription>{t.description}</CardDescription>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}

function SiteForm({ overview }: { overview: SiteOverview }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveSite(workspace.id)
  const site = overview.site

  const form = useForm({
    defaultValues: {
      url: site?.url ?? "",
      maxPages: String(site?.maxPages ?? DEFAULT_MAX_PAGES),
      schedule: site?.schedule ?? ("weekly" as SiteSchedule),
    },
    onSubmit: async ({ value }) => {
      try {
        const before = overview.latestCrawl?.id
        const out = await save.mutateAsync({
          url: value.url.trim(),
          maxPages: toInt(value.maxPages),
          schedule: value.schedule,
        })
        toast.success(
          out.latestCrawl && out.latestCrawl.id !== before
            ? t.savedCrawling
            : t.saved
        )
      } catch {
        // shown in the banner
      }
    },
  })
  const submitted = () => form.state.submissionAttempts > 0

  return (
    <form
      noValidate
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      {canManage ? null : (
        <ReadOnlyNote className="mb-0">{t.readOnly}</ReadOnlyNote>
      )}
      <fieldset disabled={!canManage} className="contents">
        <form.Field
          name="url"
          validators={{
            onChange: ({ value }) => urlError(value),
            onSubmit: ({ value }) => urlError(value),
          }}
        >
          {(field) => {
            const error = shownError(field.state.meta, submitted())
            return (
              <Row
                label={t.url}
                htmlFor="site-url"
                hint={t.urlHint}
                error={error}
              >
                <Input
                  id="site-url"
                  type="url"
                  inputMode="url"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={t.urlPlaceholder}
                  className="font-mono"
                  aria-invalid={!!error || undefined}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                />
              </Row>
            )
          }}
        </form.Field>
        <div className="grid gap-5 sm:grid-cols-2">
          <form.Field
            name="maxPages"
            validators={{
              onChange: ({ value }) => maxPagesError(value),
              onSubmit: ({ value }) => maxPagesError(value),
            }}
          >
            {(field) => {
              const error = shownError(field.state.meta, submitted())
              return (
                <Row
                  label={t.maxPages}
                  htmlFor="site-max-pages"
                  hint={t.maxPagesHint}
                  error={error}
                >
                  <Input
                    id="site-max-pages"
                    inputMode="numeric"
                    className="tabular"
                    aria-invalid={!!error || undefined}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                  />
                </Row>
              )
            }}
          </form.Field>
          <form.Field name="schedule">
            {(field) => (
              <Row label={t.schedule} htmlFor="site-schedule">
                <Select
                  items={SCHEDULES.map((s) => ({
                    value: s,
                    label: t.schedules[s],
                  }))}
                  value={field.state.value}
                  disabled={!canManage}
                  onValueChange={(v) => {
                    if (typeof v === "string")
                      field.handleChange(v as SiteSchedule)
                  }}
                >
                  <SelectTrigger id="site-schedule" className="w-full">
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
            )}
          </form.Field>
        </div>
      </fieldset>
      {save.isError ? <Banner title={errorMessage(save.error)} /> : null}
      {canManage ? (
        <div>
          <form.Subscribe selector={(s) => s.isSubmitting}>
            {(submitting) => (
              <Button type="submit" disabled={submitting}>
                {submitting ? t.saving : t.save}
              </Button>
            )}
          </form.Subscribe>
        </div>
      ) : null}
    </form>
  )
}
