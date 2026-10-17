import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { Button } from "@/components/ui/button"
import { Slider } from "@/components/ui/slider"
import { LayeredGroup, LayeredRow, LayeredSelect } from "@/features/settings/layered"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { voiceSettingsQuery, voicesQuery } from "@/queries/voice"
import { ElevenLabsVoiceSelect } from "./elevenlabs-fields"
import { engineFor, enginesFor, formatDecimal, type VoiceOverrideValues } from "./model"
import { VoiceSelect } from "./voice-select"

/**
 * The Voice rows of the pipeline and request forms: another engine,
 * voice or speed than the workspace's for the content language, each
 * shown beside what the workspace gives. The host form clears them when
 * the language changes.
 */
export function VoiceOverrideRows({
  idPrefix,
  language,
  value,
  onChange,
  readOnly = false,
}: {
  idPrefix: string
  language: string
  value: VoiceOverrideValues
  onChange: (next: VoiceOverrideValues) => void
  readOnly?: boolean
}) {
  const workspace = useWorkspace()
  const engines = useQuery(voicesQuery())
  const settings = useQuery(voiceSettingsQuery(workspace.id))
  const t = m.voice.override
  const list = enginesFor(engines.data, language)
  const stored = settings.data?.languages[language]
  const workspaceEngine = engineFor(engines.data, language, stored?.engine)
  const engine = value.engine ? engineFor(engines.data, language, value.engine) : workspaceEngine
  // Without a stored config the workspace narrates with the engine's defaults.
  const source = stored ? workspace.name : m.settings.layers.builtIn
  const engineName = (id?: string) => {
    const e = engineFor(engines.data, language, id)
    return e ? (e.cloud ? m.voice.cloudEngine(e.name) : e.name) : "…"
  }
  const workspaceVoiceId = stored?.voice ?? workspaceEngine?.defaultVoice ?? ""
  const workspaceVoice =
    workspaceEngine?.voices.find((v) => v.id === workspaceVoiceId)?.label ?? (workspaceVoiceId || "…")
  // A new engine starts from its own default voice, not the workspace's.
  const inheritedVoice = value.engine
    ? {
        value: engine?.voices.find((v) => v.id === engine.defaultVoice)?.label ?? engine?.defaultVoice ?? "…",
        source: m.settings.layers.builtIn,
      }
    : { value: workspaceVoice, source }
  const workspaceSpeed = stored?.speed ?? 1
  const speed = value.speed ? Number(value.speed) : workspaceSpeed
  const id = (k: string) => `${idPrefix}-${k}`

  return (
    <LayeredGroup title={t.title}>
      {list.length > 1 ? (
        <LayeredRow
          label={t.engine}
          htmlFor={id("engine")}
          inherited={{ value: engineName(stored?.engine), source }}
          set={!!value.engine}
          readOnly={readOnly}
          onReset={() => onChange({ ...value, engine: "", voice: "" })}
        >
          {(describedBy) => (
            <LayeredSelect
              id={id("engine")}
              value={value.engine}
              options={list.map((e) => ({ value: e.id, label: engineName(e.id) }))}
              inheritLabel={m.settings.inheritOption(engineName(stored?.engine))}
              describedBy={describedBy}
              disabled={readOnly}
              // Voices differ per engine, so a new engine starts from its default voice.
              onChange={(next) => onChange({ ...value, engine: next, voice: "" })}
            />
          )}
        </LayeredRow>
      ) : null}
      <LayeredRow
        label={t.voice}
        htmlFor={id("voice")}
        inherited={inheritedVoice}
        set={!!value.voice}
        readOnly={readOnly}
        onReset={() => onChange({ ...value, voice: "" })}
      >
        {() =>
          !engine ? (
            <p className="text-sm text-muted-foreground">{m.common.loading}</p>
          ) : engine.voices.length > 0 ? (
            <VoiceSelect
              id={id("voice")}
              className={value.voice ? "w-full font-medium" : "w-full text-muted-foreground"}
              engine={engine}
              value={value.voice}
              defaultLabel={m.settings.inheritOption(String(inheritedVoice.value))}
              disabled={readOnly}
              onChange={(voice) => onChange({ ...value, voice })}
            />
          ) : (
            <ElevenLabsVoiceSelect
              id={id("voice")}
              language={language}
              value={value.voice}
              defaultLabel={m.settings.inheritOption(String(inheritedVoice.value))}
              disabled={readOnly}
              onChange={(voice) => onChange({ ...value, voice })}
            />
          )
        }
      </LayeredRow>
      <LayeredRow
        label={t.speed}
        htmlFor={value.speed ? id("speed") : undefined}
        hint={
          value.speed && engine
            ? m.voice.speedHint(formatDecimal(engine.minSpeed), formatDecimal(engine.maxSpeed))
            : undefined
        }
        inherited={{ value: m.voice.speedValue(formatDecimal(workspaceSpeed)), source }}
        set={!!value.speed}
        readOnly={readOnly}
        onReset={() => onChange({ ...value, speed: "" })}
      >
        {() =>
          value.speed && engine ? (
            <div className="flex h-9 items-center gap-3">
              <Slider
                id={id("speed")}
                className="flex-1"
                thumbLabel={t.speed}
                valueText={m.voice.speedValue(formatDecimal(speed))}
                min={engine.minSpeed}
                max={engine.maxSpeed}
                step={0.05}
                disabled={readOnly}
                value={speed}
                onValueChange={(v) => {
                  const n = Math.round((Array.isArray(v) ? v[0] : v) * 100) / 100
                  onChange({ ...value, speed: String(n) })
                }}
              />
              <span className="w-12 text-right text-sm font-medium tabular">
                {m.voice.speedValue(formatDecimal(speed))}
              </span>
            </div>
          ) : (
            <Button
              type="button"
              variant="outline"
              className="w-full justify-between border-input bg-transparent font-normal text-muted-foreground shadow-xs dark:bg-input/30"
              disabled={readOnly}
              aria-label={t.overrideSpeed}
              onClick={() => onChange({ ...value, speed: String(workspaceSpeed) })}
            >
              {m.settings.inheritOption(m.voice.speedValue(formatDecimal(workspaceSpeed)))}
              <span className="text-xs font-medium text-foreground">{t.change}</span>
            </Button>
          )
        }
      </LayeredRow>
      <p className="px-6 pt-3 text-xs text-muted-foreground">
        <Link
          to="/w/$workspaceId/settings/voice"
          params={{ workspaceId: workspace.id }}
          className="font-medium text-foreground underline decoration-input underline-offset-4 hover:decoration-foreground"
        >
          {t.settingsLink}
        </Link>
      </p>
    </LayeredGroup>
  )
}
