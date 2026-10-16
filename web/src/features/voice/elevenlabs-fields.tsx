import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon } from "lucide-react"

import type { ElevenLabsAccount, ElevenLabsSettings } from "@/api/client"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldLabel } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Row } from "@/features/pipelines/form-parts"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { meQuery } from "@/queries/auth"
import { elevenLabsAccountQuery } from "@/queries/voice"
import { elevenLabsVoiceLabel, formatDecimal, sortElevenLabsVoices } from "./model"
import { SliderRow } from "./slider-row"

/** The select's value for "workspace default". */
const DEFAULT = "default"

/** Where an admin adds the key; others are told to ask one. */
function CredentialsHint() {
  const me = useQuery(meQuery())
  const t = m.voice.elevenlabs
  if (!me.data?.isAdmin) return <>{t.askAdmin}</>
  return (
    <Link
      to="/system"
      hash="credentials"
      className="inline-flex items-center gap-1 font-medium text-foreground underline-offset-4 hover:underline"
    >
      {t.addKey}
      <ArrowUpRightIcon className="size-3.5" />
    </Link>
  )
}

/** Why there are no account voices to pick from, or null when there are. */
export function ElevenLabsAccountState({ account }: { account: ElevenLabsAccount }) {
  const t = m.voice.elevenlabs
  if (!account.configured)
    return (
      <p className="text-sm text-muted-foreground">
        {t.notConfigured} <CredentialsHint />
      </p>
    )
  if (account.error)
    return (
      <div className="grid gap-2 text-sm">
        <p className="text-destructive">{t.failed}</p>
        <p className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 font-mono text-xs break-words">
          {account.error}
        </p>
      </div>
    )
  return null
}

/**
 * The account's voices for a language: those verified for it first. A
 * saved voice the account no longer lists stays selectable by its ID.
 * With `defaultLabel`, "" (the workspace default) is an option too.
 */
export function ElevenLabsVoiceSelect({
  id,
  language,
  value,
  onChange,
  disabled,
  defaultLabel,
}: {
  id: string
  language: string
  value: string
  onChange: (voice: string) => void
  disabled?: boolean
  defaultLabel?: string
}) {
  const workspace = useWorkspace()
  const account = useQuery(elevenLabsAccountQuery(workspace.id))
  const t = m.voice.elevenlabs

  if (account.isPending) return <Skeleton className="h-9 w-full rounded-md" />
  if (account.isError) return <p className="text-sm text-destructive">{t.failed}</p>
  const state = <ElevenLabsAccountState account={account.data} />
  if (!account.data.configured || account.data.error) return state

  const voices = sortElevenLabsVoices(account.data.voices, language)
  const verified = voices.filter((v) => v.languages.includes(language))
  const others = voices.filter((v) => !v.languages.includes(language))
  const missing = value && !voices.some((v) => v.id === value)
  const items = [
    ...(defaultLabel ? [{ value: DEFAULT, label: defaultLabel }] : []),
    ...(missing ? [{ value, label: t.unlisted(value) }] : []),
    ...voices.map((v) => ({ value: v.id, label: elevenLabsVoiceLabel(v) })),
  ]
  const groups = [
    {
      key: "verified",
      label: t.verified(m.voice.languages[language] ?? language),
      voices: verified,
    },
    { key: "others", label: t.otherVoices, voices: others },
  ].filter((g) => g.voices.length > 0)

  return (
    <Select
      items={items}
      disabled={disabled}
      value={value || (defaultLabel ? DEFAULT : null)}
      onValueChange={(v) => onChange(!v || v === DEFAULT ? "" : String(v))}
    >
      <SelectTrigger id={id} className="w-full">
        <SelectValue placeholder={t.pickVoice} />
      </SelectTrigger>
      <SelectContent>
        {defaultLabel ? <SelectItem value={DEFAULT}>{defaultLabel}</SelectItem> : null}
        {missing ? <SelectItem value={value}>{t.unlisted(value)}</SelectItem> : null}
        {groups.map((g) => (
          <SelectGroup key={g.key}>
            {groups.length > 1 ? <SelectLabel>{g.label}</SelectLabel> : null}
            {g.voices.map((v) => (
              <SelectItem key={v.id} value={v.id}>
                {elevenLabsVoiceLabel(v)}
              </SelectItem>
            ))}
          </SelectGroup>
        ))}
      </SelectContent>
    </Select>
  )
}

/** Model, delivery sliders and speaker boost, with the account's usage. */
export function ElevenLabsFields({
  idPrefix,
  language,
  value,
  onChange,
  disabled,
}: {
  idPrefix: string
  language: string
  value: ElevenLabsSettings
  onChange: (next: ElevenLabsSettings) => void
  disabled: boolean
}) {
  const workspace = useWorkspace()
  const account = useQuery(elevenLabsAccountQuery(workspace.id))
  const t = m.voice.elevenlabs
  const set = <K extends keyof ElevenLabsSettings>(key: K, v: ElevenLabsSettings[K]) =>
    onChange({ ...value, [key]: v })

  const models = (account.data?.models ?? []).filter(
    (mo) => mo.languages.length === 0 || mo.languages.includes(language)
  )
  const model = models.find((mo) => mo.id === value.model)
  const modelItems = [
    ...(model ? [] : [{ value: value.model, label: value.model }]),
    ...models.map((mo) => ({ value: mo.id, label: mo.name || mo.id })),
  ]
  const usage =
    account.data?.characterLimit !== undefined && account.data.characterCount !== undefined
      ? t.usage(
          formatInteger(account.data.characterCount),
          formatInteger(account.data.characterLimit),
          account.data.tier ?? ""
        )
      : undefined

  return (
    <div className="grid gap-5 rounded-xl border p-4">
      <div className="grid gap-1">
        <p className="font-medium">{t.title}</p>
        <p className="text-sm text-muted-foreground">{t.billing}</p>
        {usage ? <p className="tabular text-sm text-muted-foreground">{usage}</p> : null}
      </div>
      <div className="grid gap-5 sm:grid-cols-2">
        <Row
          label={t.model}
          htmlFor={`${idPrefix}-model`}
          hint={
            model
              ? t.modelHint(formatDecimal(model.costMultiplier), formatInteger(model.maxCharacters))
              : undefined
          }
        >
          <Select
            items={modelItems}
            disabled={disabled || modelItems.length === 0}
            value={value.model}
            onValueChange={(v) => v && set("model", String(v))}
          >
            <SelectTrigger id={`${idPrefix}-model`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {modelItems.map((i) => (
                <SelectItem key={i.value} value={i.value}>
                  {i.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        <div className="grid content-end pb-2">
          <Field orientation="horizontal" className="w-auto">
            <Checkbox
              id={`${idPrefix}-boost`}
              disabled={disabled || (model ? !model.canSpeakerBoost : false)}
              checked={value.speakerBoost}
              onCheckedChange={(on) => set("speakerBoost", !!on)}
            />
            <FieldLabel htmlFor={`${idPrefix}-boost`} className="font-normal">
              {t.speakerBoost}
            </FieldLabel>
          </Field>
        </div>
      </div>
      <div className="grid gap-5 sm:grid-cols-3">
        <SliderRow
          id={`${idPrefix}-stability`}
          label={t.stability}
          hint={t.stabilityHint}
          value={value.stability}
          display={formatDecimal(value.stability)}
          min={0}
          max={1}
          disabled={disabled}
          onChange={(v) => set("stability", v)}
        />
        <SliderRow
          id={`${idPrefix}-similarity`}
          label={t.similarity}
          hint={t.similarityHint}
          value={value.similarityBoost}
          display={formatDecimal(value.similarityBoost)}
          min={0}
          max={1}
          disabled={disabled}
          onChange={(v) => set("similarityBoost", v)}
        />
        <SliderRow
          id={`${idPrefix}-style`}
          label={t.style}
          hint={t.styleHint}
          value={value.style}
          display={formatDecimal(value.style)}
          min={0}
          max={1}
          disabled={disabled || (model ? !model.canStyle : false)}
          onChange={(v) => set("style", v)}
        />
      </div>
    </div>
  )
}
