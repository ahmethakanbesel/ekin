import { Fragment, useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { RotateCcwIcon } from "lucide-react"
import { toast } from "sonner"

import type { Credential } from "@/api/client"
import { Banner, ErrorState, TableSkeleton } from "@/components/page"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useRetained } from "@/lib/use-retained"
import { credentialsQuery, systemQuery, useUpdateCredentials } from "@/queries/system"

/** Display groups, in order; keys the server adds later land in "other". */
const GROUPS: Array<{ id: string; keys: string[] }> = [
  { id: "claude", keys: ["claudeOauthToken", "claudeModel"] },
  { id: "stock", keys: ["pexelsApiKey"] },
  { id: "decisions", keys: ["openrouterApiKey", "decisionModel"] },
  {
    id: "email",
    keys: ["smtpHost", "smtpPort", "smtpUsername", "smtpPassword", "smtpFrom", "smtpTls"],
  },
  { id: "youtube", keys: ["youtubeClientId", "youtubeClientSecret"] },
  { id: "openseo", keys: ["openseoApiKey", "openseoUrl"] },
  { id: "speech", keys: ["elevenlabsApiKey"] },
]

const TLS_MODES = ["starttls", "tls", "none"] as const
/** The TLS select's value for "not set". */
const TLS_UNSET = "unset"

/** What the edit control starts with: secrets empty, the rest their value. */
function initialDraft(c: Credential): string {
  return c.secret ? "" : (c.value ?? "")
}

function isChanged(c: Credential, draft: string | undefined): boolean {
  if (draft === undefined) return false
  return c.secret ? draft.trim() !== "" : draft.trim() !== initialDraft(c).trim()
}

function draftError(c: Credential, draft: string | undefined): string | undefined {
  if (c.key !== "smtpPort" || !draft?.trim()) return undefined
  const t = draft.trim()
  return /^\d+$/.test(t) && Number(t) >= 1 && Number(t) <= 65535
    ? undefined
    : m.system.wholeRange(1, 65535)
}

/** The redirect URI the YouTube check names, e.g. https://host/api/v1/oauth/youtube/callback. */
function youtubeRedirect(checks: Array<{ name: string; detail: string }> | undefined) {
  const detail = checks?.find((c) => c.name.toLowerCase().startsWith("youtube"))?.detail
  return detail ? /redirect URI (\S+)/.exec(detail)?.[1] : undefined
}

/** Admins: API keys and service settings, saved here or taken from the environment. */
export function Credentials() {
  const query = useQuery(credentialsQuery())
  const status = useQuery(systemQuery())
  const save = useUpdateCredentials()
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [removing, setRemoving] = useState<Credential | null>(null)

  const credentials = query.data ?? []
  const changed = credentials.filter((c) => isChanged(c, drafts[c.key]))
  const invalid = credentials.some((c) => draftError(c, drafts[c.key]))
  const redirect = youtubeRedirect(status.data?.checks)
  const sectionRef = useRef<HTMLElement>(null)
  const loaded = !!query.data
  // Links to /system#credentials (from the SEO settings) land here once the table exists.
  useEffect(() => {
    if (loaded && window.location.hash === "#credentials") sectionRef.current?.scrollIntoView()
  }, [loaded])

  // Forget the drafts the server now has (secrets go back to empty).
  const settle = (keys: string[]) =>
    setDrafts((prev) => {
      const next = { ...prev }
      for (const k of keys) delete next[k]
      return next
    })

  const submit = async () => {
    if (changed.length === 0 || invalid) return
    const values: Record<string, string> = {}
    for (const c of changed) values[c.key] = (drafts[c.key] ?? "").trim()
    try {
      await save.mutateAsync(values)
      settle(Object.keys(values))
      toast.success(m.system.credentialsSaved(changed.length))
    } catch {
      // shown in the banner
    }
  }

  let body
  if (query.isPending) body = <TableSkeleton rows={6} />
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else {
    const known = new Set(GROUPS.flatMap((g) => g.keys))
    const groups = [
      ...GROUPS.map((g) => ({
        id: g.id,
        rows: g.keys
          .map((k) => credentials.find((c) => c.key === k))
          .filter((c): c is Credential => !!c),
      })),
      { id: "other", rows: credentials.filter((c) => !known.has(c.key)) },
    ].filter((g) => g.rows.length > 0)

    body = (
      <form
        noValidate
        className="grid gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <div className="rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">{m.system.credentialColumns.setting}</TableHead>
                <TableHead>{m.system.credentialColumns.change}</TableHead>
                <TableHead className="w-12 pr-4">
                  <span className="sr-only">{m.common.actions}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((g) => (
                <Fragment key={g.id}>
                  <TableRow className="bg-muted/40 hover:bg-muted/40">
                    <TableCell colSpan={3} className="pl-4 whitespace-normal">
                      <p className="font-heading font-medium">{m.system.credentialGroups[g.id]}</p>
                      {g.id === "youtube" && redirect ? (
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          {m.system.youtubeRedirect}{" "}
                          <span className="font-mono break-all text-foreground select-all">
                            {redirect}
                          </span>
                        </p>
                      ) : null}
                      {g.id === "openseo" ? (
                        <p className="mt-0.5 text-xs text-muted-foreground">{m.system.openseoHint}</p>
                      ) : null}
                    </TableCell>
                  </TableRow>
                  {g.rows.map((c) => (
                    <CredentialRow
                      key={c.key}
                      credential={c}
                      draft={drafts[c.key] ?? initialDraft(c)}
                      error={draftError(c, drafts[c.key])}
                      disabled={save.isPending}
                      onDraft={(v) => setDrafts((prev) => ({ ...prev, [c.key]: v }))}
                      onRemove={() => setRemoving(c)}
                    />
                  ))}
                </Fragment>
              ))}
            </TableBody>
          </Table>
        </div>
        {save.error ? <Banner title={errorMessage(save.error)} /> : null}
        <div className="flex flex-wrap items-center justify-end gap-3">
          {changed.length > 0 ? (
            <span className="text-sm text-muted-foreground">
              {m.system.unsavedChanges(changed.length)}
            </span>
          ) : null}
          <Button type="submit" disabled={save.isPending || changed.length === 0 || invalid}>
            {save.isPending ? m.common.saving : m.common.save}
          </Button>
        </div>
      </form>
    )
  }

  return (
    <section id="credentials" ref={sectionRef} className="scroll-mt-8">
      <div className="mb-4 space-y-1">
        <h2 className="font-heading text-lg font-semibold tracking-tight">
          {m.system.credentialsTitle}
        </h2>
        <p className="text-sm text-muted-foreground">{m.system.credentialsDescription}</p>
      </div>
      {body}
      <RemoveSavedDialog
        credential={removing}
        onOpenChange={(open) => !open && setRemoving(null)}
        onRemoved={(key) => settle([key])}
      />
    </section>
  )
}

const sourceVariant = { settings: "secondary", env: "secondary", default: "outline", unset: "outline" } as const

function CredentialRow({
  credential: c,
  draft,
  error,
  disabled,
  onDraft,
  onRemove,
}: {
  credential: Credential
  draft: string
  error?: string
  disabled: boolean
  onDraft: (value: string) => void
  onRemove: () => void
}) {
  const id = `cred-${c.key}`
  let control
  if (c.secret)
    control = (
      <Input
        id={id}
        type="password"
        autoComplete="new-password"
        spellCheck={false}
        className="min-w-40 font-mono"
        placeholder={c.source === "unset" ? m.system.secretSet : m.system.secretKeep}
        aria-label={m.system.secretValue(c.label)}
        disabled={disabled}
        value={draft}
        onChange={(e) => onDraft(e.target.value)}
      />
    )
  else if (c.key === "smtpTls") {
    const items = [
      { value: TLS_UNSET, label: m.system.tlsNotSet },
      ...TLS_MODES.map((t) => ({ value: t, label: m.system.tlsOptions[t] })),
    ]
    control = (
      <Select
        items={items}
        disabled={disabled}
        value={draft || TLS_UNSET}
        onValueChange={(v) => onDraft(!v || v === TLS_UNSET ? "" : String(v))}
      >
        <SelectTrigger id={id} className="w-full min-w-40" aria-label={m.system.textValue(c.label)}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((it) => (
            <SelectItem key={it.value} value={it.value}>
              {it.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  } else
    control = (
      <Input
        id={id}
        autoComplete="off"
        spellCheck={false}
        inputMode={c.key === "smtpPort" ? "numeric" : undefined}
        className="min-w-40"
        aria-label={m.system.textValue(c.label)}
        aria-invalid={!!error || undefined}
        disabled={disabled}
        value={draft}
        onChange={(e) => onDraft(e.target.value)}
      />
    )

  return (
    <TableRow>
      <TableCell className="w-1/2 pl-4 align-top whitespace-normal">
        <div className="grid gap-1">
          <label htmlFor={id} className="font-medium">
            {c.label}
          </label>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <Badge variant={sourceVariant[c.source]} className="max-w-full truncate">
              {c.source === "env"
                ? m.system.credentialSource.env(c.env)
                : m.system.credentialSource[c.source]}
            </Badge>
            {c.value ? (
              <span
                className="min-w-0 truncate font-mono text-xs text-muted-foreground"
                title={`${m.system.credentialColumns.current}: ${c.value}`}
              >
                {c.value}
              </span>
            ) : null}
          </div>
          {c.source === "env" ? null : (
            <p className="font-mono text-xs break-all text-muted-foreground/80">{c.env}</p>
          )}
        </div>
      </TableCell>
      <TableCell className="align-top whitespace-normal">
        {control}
        {error ? <p className="mt-1 text-xs text-destructive">{error}</p> : null}
      </TableCell>
      <TableCell className="pr-4 text-right align-top">
        {c.source === "settings" ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={m.system.removeSaved}
            title={m.system.removeSaved}
            disabled={disabled}
            onClick={onRemove}
          >
            <RotateCcwIcon />
          </Button>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

function RemoveSavedDialog({
  credential,
  onOpenChange,
  onRemoved,
}: {
  credential: Credential | null
  onOpenChange: (open: boolean) => void
  onRemoved: (key: string) => void
}) {
  const target = useRetained(credential)
  const save = useUpdateCredentials()

  const confirm = async () => {
    if (!target) return
    try {
      await save.mutateAsync({ [target.key]: "" })
      onRemoved(target.key)
      toast.success(m.system.removedSaved(target.label))
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!credential} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.system.removeSavedTitle(target?.label ?? "")}</AlertDialogTitle>
          <AlertDialogDescription>{m.system.removeSavedBody(target?.env ?? "")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={save.isPending}
            onClick={() => void confirm()}
          >
            {m.system.removeSavedConfirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
