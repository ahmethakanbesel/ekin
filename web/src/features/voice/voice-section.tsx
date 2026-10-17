import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { DicesIcon, Loader2Icon, PlayIcon, PlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"

import { isApiError, type VoiceConfig, type VoiceEngine, type VoiceSettings } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Banner, ErrorState } from "@/components/page"
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
import { Row } from "@/features/pipelines/form-parts"
import { SettingsSectionHeader } from "@/features/settings/settings-page"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import {
  usePreviewVoice,
  useSaveVoiceSettings,
  voiceSettingsQuery,
  voicesQuery,
} from "@/queries/voice"
import { ElevenLabsFields, ElevenLabsVoiceSelect } from "./elevenlabs-fields"
import {
  ELEVENLABS_DEFAULTS,
  QUALITIES,
  VOICE_LIMITS,
  defaultConfig,
  engineFor,
  enginesFor,
  formatDecimal,
  fromLexicon,
  lexiconRow,
  switchEngine,
  toLexicon,
  validateLexicon,
  voiceLanguages,
  type LexiconRow,
} from "./model"
import { SliderRow } from "./slider-row"
import { VoiceSelect } from "./voice-select"

/** Settings › Voice: narration per language, with previews and pronunciations. */
export function VoiceSection() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const canEdit = useCan("edit")
  const engines = useQuery(voicesQuery())
  const settings = useQuery(voiceSettingsQuery(workspace.id))

  let body
  if (engines.isPending || settings.isPending)
    body = <Skeleton className="h-96 w-full rounded-xl" />
  else if (engines.isError || settings.isError)
    body = (
      <ErrorState
        error={engines.error ?? settings.error}
        onRetry={() => {
          void engines.refetch()
          void settings.refetch()
        }}
      />
    )
  else
    body = (
      <div className="grid gap-6">
        {voiceLanguages(engines.data).map((lang) => {
          const config = settings.data.languages[lang]
          const lexicon = settings.data.lexicon[lang]
          return (
            <LanguageCard
              // Re-mount when the stored settings of this language change.
              key={`${lang}:${JSON.stringify([config, lexicon])}`}
              language={lang}
              engines={enginesFor(engines.data, lang)}
              settings={settings.data}
              canManage={canManage}
              canEdit={canEdit}
            />
          )
        })}
      </div>
    )

  return (
    <div className="max-w-3xl">
      <SettingsSectionHeader
        section="voice"
        readOnly={canManage ? undefined : canEdit ? m.voice.readOnlyHint : m.voice.readOnlyViewer}
      />
      {body}
    </div>
  )
}

function seedText(c: VoiceConfig): string {
  return c.seed === undefined ? "" : String(c.seed)
}

function seedProblem(seed: string): string | undefined {
  const t = seed.trim()
  if (t === "") return undefined
  return /^\d+$/.test(t) && Number(t) <= VOICE_LIMITS.seed[1] ? undefined : m.voice.seedInvalid
}

/**
 * The wire config: seed only for engines with one (blank: unset), quality
 * and ElevenLabs settings only for their engines.
 */
function toConfig(engine: VoiceEngine, c: VoiceConfig, seed: string): VoiceConfig {
  const t = seed.trim()
  return {
    engine: engine.id,
    voice: c.voice,
    speed: c.speed,
    sentencePause: c.sentencePause,
    paragraphPause: c.paragraphPause,
    seed: engine.hasSeed && t !== "" && !seedProblem(t) ? Number(t) : undefined,
    quality: engine.hasQuality ? (c.quality ?? "fp32") : undefined,
    elevenlabs: engine.id === "elevenlabs" ? (c.elevenlabs ?? ELEVENLABS_DEFAULTS) : undefined,
  }
}

function engineLabel(e: VoiceEngine): string {
  return e.cloud ? m.voice.cloudEngine(e.name) : e.name
}

function LanguageCard({
  language: lang,
  engines,
  settings,
  canManage,
  canEdit,
}: {
  language: string
  engines: VoiceEngine[]
  settings: VoiceSettings
  canManage: boolean
  canEdit: boolean
}) {
  const workspace = useWorkspace()
  const save = useSaveVoiceSettings(workspace.id)
  const stored = settings.languages[lang] ?? defaultConfig(engines[0])
  const [config, setConfig] = useState<VoiceConfig>(stored)
  const engine = engineFor(engines, lang, config.engine)
  const storedEngine = engineFor(engines, lang, stored.engine)
  const [seed, setSeed] = useState(() => seedText(stored))
  const [initialRows] = useState(() => fromLexicon(settings.lexicon[lang]))
  const [rows, setRows] = useState<LexiconRow[]>(initialRows)
  const [submitted, setSubmitted] = useState(false)
  const name = m.voice.languages[lang] ?? lang
  const id = (field: string) => `voice-${lang}-${field}`
  const disabled = !canManage || save.isPending

  const seedError = seedProblem(seed)
  const effective = toConfig(engine, config, seed)
  const lexicon = validateLexicon(rows)
  const lexiconInvalid = lexicon.rows.size > 0 || !!lexicon.list
  const dirty =
    JSON.stringify(effective) !== JSON.stringify(toConfig(storedEngine, stored, seedText(stored))) ||
    JSON.stringify(toLexicon(rows)) !== JSON.stringify(toLexicon(initialRows))

  const set = <K extends keyof VoiceConfig>(key: K, value: VoiceConfig[K]) =>
    setConfig((c) => ({ ...c, [key]: value }))

  const submit = async () => {
    setSubmitted(true)
    if (seedError || lexiconInvalid) return
    try {
      await save.mutateAsync({
        languages: { ...settings.languages, [lang]: effective },
        lexicon: { ...settings.lexicon, [lang]: toLexicon(rows) },
      })
      toast.success(m.voice.saved(name))
    } catch {
      // shown in the banner
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{name}</CardTitle>
        <CardDescription>{m.voice.engine(engineLabel(storedEngine))}</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          noValidate
          className="grid gap-6"
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
        >
          <div className="grid gap-5 sm:grid-cols-2">
            <Row
              label={m.voice.engineLabel}
              htmlFor={id("engine")}
              hint={engine.cloud ? m.voice.cloudHint : m.voice.localHint}
            >
              <Select
                items={engines.map((e) => ({ value: e.id, label: engineLabel(e) }))}
                disabled={disabled || engines.length < 2}
                value={engine.id}
                onValueChange={(v) => {
                  const next = engines.find((e) => e.id === v)
                  if (next && next.id !== engine.id) setConfig((c) => switchEngine(c, next))
                }}
              >
                <SelectTrigger id={id("engine")} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {engines.map((e) => (
                    <SelectItem key={e.id} value={e.id}>
                      <span className="flex items-center gap-2">
                        {e.name}
                        {e.cloud ? <Badge variant="outline">{m.voice.cloud}</Badge> : null}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Row>
            <Row label={m.voice.voice} htmlFor={id("voice")}>
              {engine.voices.length > 0 ? (
                <VoiceSelect
                  id={id("voice")}
                  engine={engine}
                  value={config.voice}
                  onChange={(v) => v && set("voice", v)}
                  disabled={disabled}
                />
              ) : (
                <ElevenLabsVoiceSelect
                  id={id("voice")}
                  language={lang}
                  value={config.voice}
                  onChange={(v) => v && set("voice", v)}
                  disabled={disabled}
                />
              )}
            </Row>
            {engine.hasQuality ? (
              <Row label={m.voice.quality} htmlFor={id("quality")} hint={m.voice.qualityHint}>
                <Select
                  items={QUALITIES.map((q) => ({ value: q, label: m.voice.qualities[q] }))}
                  disabled={disabled}
                  value={config.quality ?? "fp32"}
                  onValueChange={(v) => v && set("quality", v as VoiceConfig["quality"])}
                >
                  <SelectTrigger id={id("quality")} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {QUALITIES.map((q) => (
                      <SelectItem key={q} value={q}>
                        {m.voice.qualities[q]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Row>
            ) : null}
            {engine.hasSeed ? (
              <Row
                label={m.voice.seed}
                htmlFor={id("seed")}
                hint={m.voice.seedHint}
                error={seedError}
              >
                <div className="flex gap-2">
                  <Input
                    id={id("seed")}
                    inputMode="numeric"
                    className="max-w-40 tabular"
                    disabled={disabled}
                    value={seed}
                    onChange={(e) => setSeed(e.target.value)}
                    aria-invalid={!!seedError || undefined}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    disabled={disabled}
                    onClick={() => setSeed(String(Math.floor(Math.random() * (VOICE_LIMITS.seed[1] + 1))))}
                  >
                    <DicesIcon data-icon="inline-start" />
                    {m.voice.newTake}
                  </Button>
                </div>
              </Row>
            ) : null}
          </div>

          <div className="grid gap-5 sm:grid-cols-3">
            <SliderRow
              id={id("speed")}
              label={m.voice.speed}
              hint={m.voice.speedHint(formatDecimal(engine.minSpeed), formatDecimal(engine.maxSpeed))}
              value={config.speed}
              display={m.voice.speedValue(formatDecimal(config.speed))}
              min={engine.minSpeed}
              max={engine.maxSpeed}
              disabled={disabled}
              onChange={(v) => set("speed", v)}
            />
            <SliderRow
              id={id("sentence")}
              label={m.voice.sentencePause}
              hint={engine.id === "elevenlabs" ? m.voice.elevenlabs.sentencePauseHint : m.voice.pauseHint}
              value={config.sentencePause}
              display={m.voice.seconds(formatDecimal(config.sentencePause))}
              min={VOICE_LIMITS.pause[0]}
              max={VOICE_LIMITS.pause[1]}
              disabled={disabled}
              onChange={(v) => set("sentencePause", v)}
            />
            <SliderRow
              id={id("paragraph")}
              label={m.voice.paragraphPause}
              hint={m.voice.pauseHint}
              value={config.paragraphPause}
              display={m.voice.seconds(formatDecimal(config.paragraphPause))}
              min={VOICE_LIMITS.pause[0]}
              max={VOICE_LIMITS.pause[1]}
              disabled={disabled}
              onChange={(v) => set("paragraphPause", v)}
            />
          </div>

          {engine.id === "elevenlabs" ? (
            <ElevenLabsFields
              idPrefix={id("el")}
              language={lang}
              value={config.elevenlabs ?? ELEVENLABS_DEFAULTS}
              onChange={(el) => set("elevenlabs", el)}
              disabled={disabled}
            />
          ) : null}

          <PreviewPanel
            language={lang}
            config={effective}
            canPreview={canEdit && !seedError}
            disabledReason={canEdit ? undefined : m.voice.previewForbidden}
          />

          <LexiconEditor
            idPrefix={id("lex")}
            rows={rows}
            onChange={setRows}
            errors={lexicon}
            showErrors={submitted}
            disabled={disabled}
          />

          {save.error ? <Banner title={errorMessage(save.error)} /> : null}
          {canManage ? (
            <div className="flex justify-end">
              <Button
                type="submit"
                variant={dirty ? "default" : "outline"}
                disabled={save.isPending || !dirty}
              >
                {save.isPending ? m.common.saving : m.voice.save}
              </Button>
            </div>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function PreviewPanel({
  language,
  config,
  canPreview,
  disabledReason,
}: {
  language: string
  config: VoiceConfig
  canPreview: boolean
  disabledReason?: string
}) {
  const workspace = useWorkspace()
  const preview = usePreviewVoice(workspace.id)
  const [text, setText] = useState("")
  const [audio, setAudio] = useState<string | null>(null)
  const abort = useRef<AbortController | null>(null)
  const tooLong = [...text.trim()].length > VOICE_LIMITS.sample
  const id = `voice-${language}-sample`

  // Stop polling and free the audio when the card goes away.
  useEffect(
    () => () => {
      abort.current?.abort()
    },
    []
  )
  useEffect(() => () => (audio ? URL.revokeObjectURL(audio) : undefined), [audio])

  const run = async () => {
    abort.current?.abort()
    const controller = new AbortController()
    abort.current = controller
    try {
      const blob = await preview.mutateAsync({
        language: language as "tr" | "en",
        config,
        text: text.trim(),
        signal: controller.signal,
      })
      setAudio(URL.createObjectURL(blob))
    } catch {
      // shown below
    }
  }

  let failure: string | undefined
  if (preview.error && !(preview.error instanceof DOMException)) {
    if (isApiError(preview.error, "PreviewTimeout")) failure = m.voice.previewTimeout
    else if (isApiError(preview.error, "PreviewFailed"))
      failure = preview.error.message || m.voice.previewFailed
    else failure = errorMessage(preview.error)
  }

  return (
    <div className="grid gap-3 rounded-xl border bg-muted/30 p-4">
      <p className="font-medium">{m.voice.previewTitle}</p>
      <Row
        label={m.voice.sampleLabel}
        htmlFor={id}
        hint={m.voice.sampleHint}
        error={tooLong ? m.voice.sampleTooLong : undefined}
      >
        <Textarea
          id={id}
          rows={3}
          className="min-h-20 bg-background"
          placeholder={m.voice.samples[language]}
          value={text}
          onChange={(e) => setText(e.target.value)}
          aria-invalid={tooLong || undefined}
        />
      </Row>
      <div className="flex flex-wrap items-center gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={!canPreview || tooLong || preview.isPending}
          title={disabledReason}
          onClick={() => void run()}
        >
          {preview.isPending ? (
            <Loader2Icon data-icon="inline-start" className="animate-spin" />
          ) : (
            <PlayIcon data-icon="inline-start" />
          )}
          {preview.isPending ? m.voice.previewing : m.voice.preview}
        </Button>
        {preview.isPending ? (
          <span className="text-sm text-muted-foreground" role="status">
            {m.voice.previewWaiting}
          </span>
        ) : null}
        {audio && !preview.isPending ? (
          // oxlint-disable-next-line jsx-a11y/media-has-caption
          <audio controls autoPlay src={audio} className="h-10 max-w-full min-w-64 flex-1" />
        ) : null}
      </div>
      {failure ? <Banner title={m.voice.previewFailed}>{failure}</Banner> : null}
    </div>
  )
}

function LexiconEditor({
  idPrefix,
  rows,
  onChange,
  errors,
  showErrors,
  disabled,
}: {
  idPrefix: string
  rows: LexiconRow[]
  onChange: (rows: LexiconRow[]) => void
  errors: ReturnType<typeof validateLexicon>
  showErrors: boolean
  disabled: boolean
}) {
  const update = (id: number, patch: Partial<LexiconRow>) =>
    onChange(rows.map((r) => (r.id === id ? { ...r, ...patch } : r)))

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="font-medium">{m.voice.lexiconTitle}</p>
        <span
          className={cn(
            "text-xs text-muted-foreground tabular",
            rows.length > VOICE_LIMITS.lexicon && "font-medium text-destructive"
          )}
        >
          {m.voice.lexiconCount(rows.length, VOICE_LIMITS.lexicon)}
        </span>
      </div>
      <p className="text-sm text-muted-foreground">{m.voice.lexiconHint}</p>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">{m.voice.lexiconEmpty}</p>
      ) : (
        <div className="grid gap-2">
          <div className="grid grid-cols-[1fr_1fr_2rem] gap-2 text-xs font-medium text-muted-foreground">
            <span>{m.voice.written}</span>
            <span>{m.voice.spoken}</span>
          </div>
          {rows.map((r) => {
            const e = errors.rows.get(r.id)
            // A row being typed shows its errors once both sides have text, or after a save attempt.
            const show = showErrors || (r.written.trim() !== "" && r.spoken.trim() !== "")
            const wErr = show ? e?.written : undefined
            const sErr = show ? e?.spoken : undefined
            return (
              <div key={r.id} className="grid grid-cols-[1fr_1fr_2rem] items-start gap-2">
                <div className="grid gap-1">
                  <Input
                    aria-label={`${m.voice.written} ${r.written}`.trim()}
                    id={`${idPrefix}-${r.id}-w`}
                    autoComplete="off"
                    spellCheck={false}
                    placeholder={m.voice.writtenPlaceholder}
                    disabled={disabled}
                    value={r.written}
                    onChange={(ev) => update(r.id, { written: ev.target.value })}
                    aria-invalid={!!wErr || undefined}
                  />
                  {wErr ? <p className="text-xs text-destructive">{wErr}</p> : null}
                </div>
                <div className="grid gap-1">
                  <Input
                    aria-label={`${m.voice.spoken} ${r.written}`.trim()}
                    autoComplete="off"
                    spellCheck={false}
                    placeholder={m.voice.spokenPlaceholder}
                    disabled={disabled}
                    value={r.spoken}
                    onChange={(ev) => update(r.id, { spoken: ev.target.value })}
                    aria-invalid={!!sErr || undefined}
                  />
                  {sErr ? <p className="text-xs text-destructive">{sErr}</p> : null}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="mt-0.5"
                  aria-label={m.voice.removeWord(r.written.trim())}
                  title={m.voice.removeWord(r.written.trim())}
                  disabled={disabled}
                  onClick={() => onChange(rows.filter((x) => x.id !== r.id))}
                >
                  <XIcon />
                </Button>
              </div>
            )
          })}
        </div>
      )}
      {errors.list ? <p className="text-sm text-destructive">{errors.list}</p> : null}
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled || rows.length >= VOICE_LIMITS.lexicon}
          onClick={() => onChange([...rows, lexiconRow()])}
        >
          <PlusIcon data-icon="inline-start" />
          {m.voice.addWord}
        </Button>
      </div>
    </div>
  )
}
