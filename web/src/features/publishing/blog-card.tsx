import { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { NewspaperIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import type { BlogInput, BlogTarget } from "@/api/client"
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
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Switch } from "@/components/ui/switch"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useDeleteBlog, useSaveBlog } from "@/queries/integrations"

type Kind = BlogInput["kind"]
const KINDS: Kind[] = ["github", "dir"]

export const DEFAULT_PATH_TEMPLATE = "/{locale}blog/{slug}/"
const DEFAULT_LOCALE = "tr"

type Values = {
  kind: Kind
  repo: string
  branch: string
  token: string
  dir: string
  contentDir: string
  publicDir: string
  siteUrl: string
  pathTemplate: string
  defaultLocale: string
  author: string
  draft: boolean
}

function initialValues(blog: BlogTarget | undefined): Values {
  return {
    kind: blog?.kind ?? "github",
    repo: blog?.repo ?? "",
    branch: blog?.branch || "main",
    token: "",
    dir: blog?.dir ?? "",
    contentDir: blog?.contentDir ?? "",
    publicDir: blog?.publicDir ?? "",
    siteUrl: blog?.siteUrl ?? "",
    pathTemplate: blog?.pathTemplate || DEFAULT_PATH_TEMPLATE,
    defaultLocale: blog?.defaultLocale || DEFAULT_LOCALE,
    author: blog?.author ?? "",
    draft: blog?.draft ?? false,
  }
}

function toInput(v: Values): BlogInput {
  const common = {
    kind: v.kind,
    contentDir: v.contentDir.trim(),
    publicDir: v.publicDir.trim(),
    siteUrl: v.siteUrl.trim(),
    pathTemplate: v.pathTemplate.trim(),
    defaultLocale: v.defaultLocale.trim(),
    author: v.author.trim(),
    draft: v.draft,
  }
  if (v.kind === "dir") return { ...common, dir: v.dir.trim() }
  const token = v.token.trim()
  // An omitted token keeps the stored one.
  return { ...common, repo: v.repo.trim(), branch: v.branch.trim(), ...(token ? { token } : {}) }
}

// The server's rules (BlogTarget.Validate), checked here for quick feedback.
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/
const v = m.blog.validation
const repoError = (s: string) => (REPO.test(s.trim()) ? undefined : v.repo)
const dirError = (s: string) => (s.trim().startsWith("/") ? undefined : v.dir)
const insideError = (s: string) => (s.split("/").includes("..") ? v.outside : undefined)
const contentDirError = (s: string) => (s.trim() ? insideError(s) : v.contentDir)
const templateError = (s: string) => (!s.trim() || s.includes("{slug}") ? undefined : v.template)
const siteUrlError = (s: string) =>
  !s.trim() || /^https?:\/\//.test(s.trim()) ? undefined : v.siteUrl

/** `{locale}` is empty for the default locale and "xx/" for the others. */
function exampleUrl(siteUrl: string, template: string, locale: string) {
  const path = (template.trim() || DEFAULT_PATH_TEMPLATE)
    .replaceAll("{locale}", locale)
    .replaceAll("{slug}", "my-post")
  return siteUrl.trim().replace(/\/+$/, "") + path
}

export function BlogCard({ blog }: { blog: BlogTarget | undefined }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const save = useSaveBlog(workspace.id)
  const [removing, setRemoving] = useState(false)
  const hasToken = blog?.hasToken ?? false

  const form = useForm({
    defaultValues: initialValues(blog),
    onSubmit: async ({ value, formApi }) => {
      try {
        await save.mutateAsync(toInput(value))
        toast.success(m.blog.saved)
        // The token is write-only: never keep it around after saving.
        formApi.reset({ ...value, token: "" })
      } catch {
        // shown in the banner
      }
    },
  })

  const textField = (
    name: Exclude<keyof Values, "kind" | "draft">,
    opts: {
      label: string
      hint?: string
      placeholder?: string
      validate?: (s: string, values: Values) => string | undefined
      type?: string
      mono?: boolean
    }
  ) => (
    <form.Field
      name={name}
      validators={
        opts.validate
          ? {
              onChangeListenTo: ["kind"],
              onChange: ({ value, fieldApi }) =>
                opts.validate?.(value as string, fieldApi.form.state.values),
              onSubmit: ({ value, fieldApi }) =>
                opts.validate?.(value as string, fieldApi.form.state.values),
            }
          : undefined
      }
    >
      {(field) => {
        const error = shownError(field.state.meta, form.state.submissionAttempts > 0)
        const id = `blog-${name}`
        return (
          <Row label={opts.label} htmlFor={id} hint={opts.hint} error={error}>
            <Input
              id={id}
              type={opts.type ?? "text"}
              autoComplete="off"
              spellCheck={false}
              placeholder={opts.placeholder}
              className={opts.mono ? "font-mono text-[13px] md:text-[13px]" : undefined}
              value={field.state.value as string}
              onChange={(e) => field.handleChange(e.target.value)}
              onBlur={field.handleBlur}
              aria-invalid={!!error || undefined}
            />
          </Row>
        )
      }}
    </form.Field>
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <NewspaperIcon className="size-4 text-muted-foreground" />
          {m.blog.title}
        </CardTitle>
        <CardDescription>{m.blog.description}</CardDescription>
        {blog && canManage ? (
          <CardAction>
            <Button variant="ghost" size="sm" onClick={() => setRemoving(true)}>
              <Trash2Icon data-icon="inline-start" />
              {m.blog.remove}
            </Button>
          </CardAction>
        ) : null}
      </CardHeader>
      <CardContent>
        {!blog && !canManage ? (
          <p className="rounded-3xl border border-dashed px-4 py-6 text-sm text-muted-foreground">
            {m.blog.notSetReadOnly}
          </p>
        ) : (
          <form
            noValidate
            className="grid gap-6"
            onSubmit={(e) => {
              e.preventDefault()
              void form.handleSubmit()
            }}
          >
            {blog ? null : <p className="text-sm text-muted-foreground">{m.blog.notSet}</p>}
            <fieldset disabled={!canManage} className="contents">
              <form.Field name="kind">
                {(field) => (
                  <Field className="gap-2">
                    <FieldLabel>{m.blog.kind}</FieldLabel>
                    <RadioGroup
                      disabled={!canManage}
                      value={field.state.value}
                      onValueChange={(next) => field.handleChange(next as Kind)}
                      className="grid gap-3 md:grid-cols-2"
                      aria-label={m.blog.kind}
                    >
                      {KINDS.map((k) => (
                        <FieldLabel key={k} htmlFor={`blog-kind-${k}`}>
                          <Field orientation="horizontal">
                            <FieldContent>
                              <FieldTitle>{m.blog.kinds[k]}</FieldTitle>
                              <FieldDescription>{m.blog.kindHints[k]}</FieldDescription>
                            </FieldContent>
                            <RadioGroupItem id={`blog-kind-${k}`} value={k} />
                          </Field>
                        </FieldLabel>
                      ))}
                    </RadioGroup>
                  </Field>
                )}
              </form.Field>

              <form.Subscribe selector={(s) => s.values.kind}>
                {(kind) =>
                  kind === "github" ? (
                    <div className="grid gap-5 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
                      {textField("repo", {
                        label: m.blog.repo,
                        placeholder: m.blog.repoPlaceholder,
                        hint: m.blog.repoHint,
                        validate: (s, all) => (all.kind === "github" ? repoError(s) : undefined),
                        mono: true,
                      })}
                      {textField("branch", {
                        label: m.blog.branch,
                        placeholder: "main",
                        hint: m.blog.branchHint,
                        mono: true,
                      })}
                      <div className="md:col-span-2">
                        {textField("token", {
                          label: m.blog.token,
                          type: "password",
                          placeholder: hasToken ? m.blog.tokenStoredPlaceholder : "github_pat_…",
                          hint: hasToken ? m.blog.tokenStored : m.blog.tokenHint,
                          validate: (s, all) =>
                            all.kind === "github" && !hasToken && !s.trim()
                              ? v.token
                              : undefined,
                          mono: true,
                        })}
                      </div>
                    </div>
                  ) : (
                    textField("dir", {
                      label: m.blog.dir,
                      placeholder: m.blog.dirPlaceholder,
                      hint: m.blog.dirHint,
                      validate: (s, all) => (all.kind === "dir" ? dirError(s) : undefined),
                      mono: true,
                    })
                  )
                }
              </form.Subscribe>

              <div className="grid gap-5 md:grid-cols-2">
                {textField("contentDir", {
                  label: m.blog.contentDir,
                  placeholder: m.blog.contentDirPlaceholder,
                  hint: m.blog.contentDirHint,
                  validate: contentDirError,
                  mono: true,
                })}
                {textField("publicDir", {
                  label: m.blog.publicDir,
                  placeholder: m.blog.publicDirPlaceholder,
                  hint: m.blog.publicDirHint,
                  validate: (s) => insideError(s),
                  mono: true,
                })}
                {textField("siteUrl", {
                  label: m.blog.siteUrl,
                  placeholder: m.blog.siteUrlPlaceholder,
                  hint: m.blog.siteUrlHint,
                  validate: siteUrlError,
                  type: "url",
                })}
                {textField("defaultLocale", {
                  label: m.blog.defaultLocale,
                  placeholder: DEFAULT_LOCALE,
                  hint: m.blog.defaultLocaleHint,
                  mono: true,
                })}
                <div className="grid gap-2 md:col-span-2">
                  {textField("pathTemplate", {
                    label: m.blog.pathTemplate,
                    placeholder: DEFAULT_PATH_TEMPLATE,
                    hint: m.blog.pathTemplateHint,
                    validate: templateError,
                    mono: true,
                  })}
                  <form.Subscribe
                    selector={(s) =>
                      [s.values.siteUrl, s.values.pathTemplate, s.values.defaultLocale] as const
                    }
                  >
                    {([siteUrl, template, locale]) => {
                      const other = (locale.trim() || DEFAULT_LOCALE) === "en" ? "tr" : "en"
                      return (
                        <p className="font-mono text-xs break-all text-muted-foreground">
                          {m.blog.examples(
                            exampleUrl(siteUrl, template, ""),
                            exampleUrl(siteUrl, template, `${other}/`)
                          )}
                        </p>
                      )
                    }}
                  </form.Subscribe>
                </div>
                {textField("author", { label: m.blog.author, hint: m.blog.authorHint })}
              </div>

              <form.Field name="draft">
                {(field) => (
                  <Field orientation="horizontal">
                    <FieldContent>
                      <FieldLabel htmlFor="blog-draft">{m.blog.draft}</FieldLabel>
                      <FieldDescription>{m.blog.draftHint}</FieldDescription>
                    </FieldContent>
                    <Switch
                      id="blog-draft"
                      disabled={!canManage}
                      checked={field.state.value}
                      onCheckedChange={(next) => field.handleChange(next)}
                    />
                  </Field>
                )}
              </form.Field>
            </fieldset>

            {save.error ? <Banner title={errorMessage(save.error)} /> : null}
            {canManage ? (
              <div className="flex justify-end">
                <form.Subscribe selector={(s) => [s.isSubmitting, s.isDirty] as const}>
                  {([submitting, dirty]) => (
                    <Button type="submit" disabled={submitting || (!!blog && !dirty)}>
                      {submitting ? m.common.saving : m.blog.save}
                    </Button>
                  )}
                </form.Subscribe>
              </div>
            ) : null}
          </form>
        )}
      </CardContent>
      <RemoveBlogDialog open={removing} onOpenChange={setRemoving} />
    </Card>
  )
}

function RemoveBlogDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const workspace = useWorkspace()
  const remove = useDeleteBlog(workspace.id)

  const confirm = async () => {
    try {
      await remove.mutateAsync()
      toast.success(m.blog.removed)
      onOpenChange(false)
    } catch {
      // shown in the dialog
    }
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) remove.reset()
        onOpenChange(next)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.blog.removeTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.blog.removeBody}</AlertDialogDescription>
        </AlertDialogHeader>
        {remove.error ? <Banner title={errorMessage(remove.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => void confirm()}
          >
            {m.blog.remove}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
