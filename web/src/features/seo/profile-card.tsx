import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Language, OpenSEOStatus, SEOProfile } from "@/api/client"
import { ChipInput } from "@/components/chip-input"
import { Banner, ErrorState, ReadOnlyNote } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Slider } from "@/components/ui/slider"
import { Textarea } from "@/components/ui/textarea"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { toInt } from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatInteger } from "@/lib/format"
import { openSeoStatusQuery, seoProfileQuery, useSaveSeoProfile } from "@/queries/seo"
import {
  DEFAULT_MAX_DIFFICULTY,
  MAX_COMPETITORS,
  MAX_SEEDS,
  countryOf,
  estimateCredits,
  isDomain,
  marketLabel,
  normalizeDomain,
} from "./openseo"

export const LANGUAGES: Language[] = ["tr", "en"]
export const languageItems = LANGUAGES.map((l) => ({
  value: l,
  label: m.values.language[l],
}))
export const isLanguage = (v: unknown): v is Language => LANGUAGES.includes(v as Language)

const countryError = (s: string) =>
  /^[A-Za-z]{2}$/.test(s.trim()) ? undefined : m.seo.countryInvalid
const minVolumeError = (s: string) =>
  s.trim() === "" || toInt(s) !== undefined ? undefined : m.seo.minVolumeInvalid

const cleanDomain = (s: string) =>
  s
    .trim()
    .replace(/^https?:\/\//, "")
    .replace(/\/+$/, "")

type ProfileValues = {
  domain: string
  country: string
  language: Language
  topics: string
  projectId: string
  seeds: string[]
  competitors: string[]
  maxDifficulty: number
  minVolume: string
}

function toValues(p: SEOProfile): ProfileValues {
  return {
    domain: p.domain,
    country: p.country,
    language: p.language,
    topics: p.topics,
    projectId: p.projectId ?? "",
    seeds: p.seeds ?? [],
    competitors: p.competitors ?? [],
    maxDifficulty: p.maxDifficulty || DEFAULT_MAX_DIFFICULTY,
    minVolume: p.minVolume ? String(p.minVolume) : "",
  }
}

/** The site OpenSEO researches when refreshing opportunities. Owners edit. */
export function SeoProfileCard() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const query = useQuery(seoProfileQuery(workspace.id))
  // Only owners may ask; for others the connection is unknown.
  const status = useQuery({
    ...openSeoStatusQuery(workspace.id),
    enabled: canManage,
  })

  let body
  if (query.isPending || (canManage && status.isPending))
    body = <Skeleton className="h-64 w-full rounded-xl" />
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  // Re-mount the form when the stored profile changes underneath it.
  else
    body = (
      <ProfileForm key={JSON.stringify(query.data)} profile={query.data} status={status.data} />
    )

  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.seo.profileTitle}</CardTitle>
        <CardDescription>{m.seo.profileDescription}</CardDescription>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}

function ProfileForm({
  profile,
  status,
}: {
  profile: SEOProfile
  status: OpenSEOStatus | undefined
}) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveSeoProfile(workspace.id)
  // Direct research needs a working key; then a project is required.
  const direct = !!status?.configured && !status.error
  const projects = status?.projects ?? []
  const projectError = (id: string) => (direct && !id ? m.seo.projectRequired : undefined)

  const form = useForm({
    defaultValues: toValues(profile),
    onSubmit: async ({ value }) => {
      try {
        await save.mutateAsync({
          domain: cleanDomain(value.domain),
          country: value.country.trim().toUpperCase(),
          language: value.language,
          topics: value.topics.trim(),
          projectId: value.projectId,
          seeds: value.seeds,
          competitors: value.competitors,
          maxDifficulty: value.maxDifficulty,
          minVolume: toInt(value.minVolume) ?? 0,
        })
        toast.success(m.seo.profileSaved)
      } catch {
        // shown in the banner
      }
    },
  })
  const submitted = () => form.state.submissionAttempts > 0

  /** Fills an empty domain and country from the chosen project. */
  const prefill = (projectId: string) => {
    const p = projects.find((x) => x.id === projectId)
    if (!p) return
    if (!form.getFieldValue("domain").trim() && p.domain)
      form.setFieldValue("domain", cleanDomain(p.domain))
    const country = countryOf(p.locationCode)
    if (!form.getFieldValue("country").trim() && country) form.setFieldValue("country", country)
  }

  return (
    <form
      noValidate
      className="grid gap-5"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      {canManage ? null : <ReadOnlyNote className="mb-0">{m.seo.profileReadOnly}</ReadOnlyNote>}
      <fieldset disabled={!canManage} className="contents">
        {direct ? (
          <form.Field
            name="projectId"
            validators={{
              onChange: ({ value }) => projectError(value),
              onSubmit: ({ value }) => projectError(value),
            }}
          >
            {(field) => {
              const error = shownError(field.state.meta, submitted())
              const known = projects.some((p) => p.id === field.state.value)
              const items = [
                ...projects.map((p) => ({ value: p.id, label: p.name })),
                ...(field.state.value && !known
                  ? [
                      {
                        value: field.state.value,
                        label: m.seo.projectUnknown(field.state.value),
                      },
                    ]
                  : []),
              ]
              return (
                <Row
                  label={m.seo.project}
                  htmlFor="seo-project"
                  hint={projects.length === 0 ? m.seo.projectsNone : m.seo.projectHint}
                  error={error}
                >
                  <Select
                    items={items}
                    value={field.state.value || null}
                    disabled={!canManage || items.length === 0}
                    onValueChange={(v) => {
                      if (typeof v !== "string") return
                      field.handleChange(v)
                      prefill(v)
                    }}
                  >
                    <SelectTrigger
                      id="seo-project"
                      className="w-full"
                      aria-invalid={!!error || undefined}
                    >
                      <SelectValue placeholder={m.seo.projectPlaceholder} />
                    </SelectTrigger>
                    <SelectContent>
                      {projects.map((p) => (
                        <SelectItem key={p.id} value={p.id}>
                          <span className="grid min-w-0">
                            <span className="truncate">{p.name}</span>
                            <span className="truncate text-xs text-muted-foreground">
                              {[p.domain, marketLabel(p)].filter(Boolean).join(" · ")}
                            </span>
                          </span>
                        </SelectItem>
                      ))}
                      {field.state.value && !known ? (
                        <SelectItem value={field.state.value}>
                          {m.seo.projectUnknown(field.state.value)}
                        </SelectItem>
                      ) : null}
                    </SelectContent>
                  </Select>
                </Row>
              )
            }}
          </form.Field>
        ) : null}
        <form.Field name="domain">
          {(field) => (
            <Row label={m.seo.domain} htmlFor="seo-domain" hint={m.seo.domainHint}>
              <Input
                id="seo-domain"
                autoComplete="off"
                spellCheck={false}
                placeholder={m.seo.domainPlaceholder}
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
              />
            </Row>
          )}
        </form.Field>
        <div className="grid grid-cols-2 gap-4">
          <form.Field
            name="country"
            validators={{
              onChange: ({ value }) => countryError(value),
              onSubmit: ({ value }) => countryError(value),
            }}
          >
            {(field) => {
              const error = shownError(field.state.meta, submitted())
              return (
                <Row
                  label={m.seo.country}
                  htmlFor="seo-country"
                  hint={m.seo.countryHint}
                  error={error}
                >
                  <Input
                    id="seo-country"
                    autoComplete="off"
                    maxLength={2}
                    className="uppercase"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value.toUpperCase())}
                    onBlur={field.handleBlur}
                    aria-invalid={!!error || undefined}
                  />
                </Row>
              )
            }}
          </form.Field>
          <form.Field name="language">
            {(field) => (
              <Row label={m.seo.language} htmlFor="seo-language">
                <Select
                  items={languageItems}
                  value={field.state.value}
                  disabled={!canManage}
                  onValueChange={(v) => isLanguage(v) && field.handleChange(v)}
                >
                  <SelectTrigger id="seo-language" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {languageItems.map((it) => (
                      <SelectItem key={it.value} value={it.value}>
                        {it.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Row>
            )}
          </form.Field>
        </div>
        <form.Field name="topics">
          {(field) => (
            <Row label={m.seo.topics} htmlFor="seo-topics" hint={m.seo.topicsHint}>
              <Textarea
                id="seo-topics"
                rows={4}
                placeholder={m.seo.topicsPlaceholder}
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
              />
            </Row>
          )}
        </form.Field>

        <Separator />
        <h3 className="-mb-1 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          {m.seo.directTitle}
        </h3>
        {status && !direct ? (
          <p className="-mt-2 text-sm text-muted-foreground">{m.seo.directInactive}</p>
        ) : null}
        <form.Field name="seeds">
          {(field) => (
            <Row label={m.seo.seeds} htmlFor="seo-seeds" hint={m.seo.seedsHint(MAX_SEEDS)}>
              <ChipInput
                id="seo-seeds"
                value={field.state.value}
                onChange={(v) => field.handleChange(v)}
                normalize={(s) => s.trim().split(/\s+/).join(" ")}
                max={MAX_SEEDS}
                disabled={!canManage}
                placeholder={m.seo.seedsPlaceholder}
              />
            </Row>
          )}
        </form.Field>
        <form.Field name="competitors">
          {(field) => (
            <Row
              label={m.seo.competitors}
              htmlFor="seo-competitors"
              hint={m.seo.competitorsHint(MAX_COMPETITORS)}
            >
              <ChipInput
                id="seo-competitors"
                value={field.state.value}
                onChange={(v) => field.handleChange(v)}
                normalize={normalizeDomain}
                validate={(d) => (isDomain(d) ? undefined : m.seo.competitorInvalid)}
                max={MAX_COMPETITORS}
                disabled={!canManage}
                placeholder={m.seo.competitorsPlaceholder}
              />
            </Row>
          )}
        </form.Field>
        <form.Field name="maxDifficulty">
          {(field) => (
            <div className="grid gap-2">
              <div className="flex items-baseline justify-between gap-2">
                <span id="seo-max-difficulty" className="text-sm font-medium">
                  {m.seo.maxDifficulty}
                </span>
                <span className="tabular text-sm text-muted-foreground">{field.state.value}</span>
              </div>
              <Slider
                aria-labelledby="seo-max-difficulty"
                thumbLabel={m.seo.maxDifficulty}
                valueText={String(field.state.value)}
                min={1}
                max={100}
                step={1}
                disabled={!canManage}
                value={field.state.value}
                onValueChange={(v) => field.handleChange(Math.round(Array.isArray(v) ? v[0] : v))}
              />
              <p className="text-sm text-muted-foreground">{m.seo.maxDifficultyHint}</p>
            </div>
          )}
        </form.Field>
        <form.Field
          name="minVolume"
          validators={{
            onChange: ({ value }) => minVolumeError(value),
            onSubmit: ({ value }) => minVolumeError(value),
          }}
        >
          {(field) => {
            const error = shownError(field.state.meta, submitted())
            return (
              <Row
                label={m.seo.minVolume}
                htmlFor="seo-min-volume"
                hint={m.seo.minVolumeHint}
                error={error}
              >
                <Input
                  id="seo-min-volume"
                  type="number"
                  inputMode="numeric"
                  min={0}
                  step={10}
                  placeholder="0"
                  className="tabular max-w-32"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={!!error || undefined}
                />
              </Row>
            )
          }}
        </form.Field>
        <form.Subscribe
          selector={(s) =>
            [s.values.seeds, s.values.topics, s.values.competitors, s.isDirty] as const
          }
        >
          {([seeds, topics, competitors, dirty]) => {
            // The server's figure until something changes; then the same formula, live.
            const live = estimateCredits(seeds, topics, competitors)
            const credits = dirty ? live : (profile.estimatedCredits ?? live)
            // Without a key the plugin path spends what the agent decides; no estimate.
            if (status && !direct) return null
            return (
              <p className="rounded-lg bg-muted/50 px-3 py-2 text-sm" aria-live="polite">
                {credits > 0 ? m.seo.estimate(formatInteger(credits)) : m.seo.estimateNone}
              </p>
            )
          }}
        </form.Subscribe>
      </fieldset>
      {save.error ? <Banner title={errorMessage(save.error)} /> : null}
      {canManage ? (
        <div className="flex justify-end">
          <form.Subscribe selector={(s) => [s.isSubmitting, s.isDirty] as const}>
            {([submitting, dirty]) => (
              <Button type="submit" disabled={submitting || !dirty}>
                {submitting ? m.common.saving : m.common.save}
              </Button>
            )}
          </form.Subscribe>
        </div>
      ) : null}
    </form>
  )
}
