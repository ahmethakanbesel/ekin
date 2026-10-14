import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Language, SEOProfile } from "@/api/client"
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
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { seoProfileQuery, useSaveSeoProfile } from "@/queries/seo"

export const LANGUAGES: Language[] = ["tr", "en"]
export const languageItems = LANGUAGES.map((l) => ({ value: l, label: m.values.language[l] }))
export const isLanguage = (v: unknown): v is Language => LANGUAGES.includes(v as Language)

const countryError = (s: string) => (/^[A-Za-z]{2}$/.test(s.trim()) ? undefined : m.seo.countryInvalid)

/** The site OpenSEO researches when refreshing opportunities. Owners edit. */
export function SeoProfileCard() {
  const workspace = useWorkspace()
  const query = useQuery(seoProfileQuery(workspace.id))

  let body
  if (query.isPending) body = <Skeleton className="h-64 w-full rounded-3xl" />
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  // Re-mount the form when the stored profile changes underneath it.
  else body = <ProfileForm key={JSON.stringify(query.data)} profile={query.data} />

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

function ProfileForm({ profile }: { profile: SEOProfile }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveSeoProfile(workspace.id)
  const form = useForm({
    defaultValues: profile,
    onSubmit: async ({ value }) => {
      try {
        await save.mutateAsync({
          domain: value.domain
            .trim()
            .replace(/^https?:\/\//, "")
            .replace(/\/+$/, ""),
          country: value.country.trim().toUpperCase(),
          language: value.language,
          topics: value.topics.trim(),
        })
        toast.success(m.seo.profileSaved)
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
      {canManage ? null : <ReadOnlyNote className="mb-0">{m.seo.profileReadOnly}</ReadOnlyNote>}
      <fieldset disabled={!canManage} className="contents">
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
                <Row label={m.seo.country} htmlFor="seo-country" hint={m.seo.countryHint} error={error}>
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
