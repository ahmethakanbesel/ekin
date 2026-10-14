import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronDownIcon } from "lucide-react"

import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldLabel } from "@/components/ui/field"
import { Slider } from "@/components/ui/slider"
import { Row } from "@/features/pipelines/form-parts"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { voicesQuery } from "@/queries/voice"
import { engineFor, formatDecimal, type VoiceOverrideValues } from "./model"
import { VoiceSelect } from "./voice-select"

/**
 * "Voice (optional)" in the pipeline and request forms: another voice or
 * speed than the workspace's, for the content language. Collapsed unless
 * an override is set. The host form clears it when the language changes.
 */
export function VoiceOverrideSection({
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
  const engine = engineFor(engines.data, language)
  const set = !!value.voice || !!value.speed
  const [open, setOpen] = useState(set)
  const t = m.voice.override
  const contentId = `${idPrefix}-content`
  const speed = value.speed ? Number(value.speed) : 1
  const voiceLabel = value.voice
    ? (engine?.voices.find((v) => v.id === value.voice)?.label ?? value.voice)
    : t.workspaceDefault
  const speedLabel = value.speed ? m.voice.speedValue(formatDecimal(speed)) : t.workspaceDefault

  return (
    <Card>
      <CardHeader>
        <button
          type="button"
          className="-m-2 flex items-start gap-3 rounded-2xl p-2 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/30"
          aria-expanded={open}
          aria-controls={contentId}
          onClick={() => setOpen((o) => !o)}
        >
          <span className="min-w-0 flex-1 space-y-1">
            <span className="block font-heading text-base font-medium">{t.title}</span>
            <span className="block text-sm text-muted-foreground">
              {set ? t.summary(voiceLabel, speedLabel) : t.description}
            </span>
          </span>
          <ChevronDownIcon
            className={cn("mt-1 size-4 shrink-0 transition-transform", open && "rotate-180")}
            aria-hidden
          />
          <span className="sr-only">{open ? t.hide : t.show}</span>
        </button>
      </CardHeader>
      {open ? (
        <CardContent id={contentId}>
          <fieldset disabled={readOnly} className="grid gap-5 sm:grid-cols-2">
            <Row label={t.voice} htmlFor={`${idPrefix}-voice`}>
              {engine ? (
                <VoiceSelect
                  id={`${idPrefix}-voice`}
                  engine={engine}
                  value={value.voice}
                  defaultLabel={t.workspaceDefault}
                  disabled={readOnly}
                  onChange={(voice) => onChange({ ...value, voice })}
                />
              ) : (
                <p className="text-sm text-muted-foreground">{m.common.loading}</p>
              )}
            </Row>
            <div className="grid content-start gap-2">
              <Field orientation="horizontal" className="w-auto">
                <Checkbox
                  id={`${idPrefix}-speed-on`}
                  disabled={readOnly}
                  checked={!!value.speed}
                  onCheckedChange={(on) => onChange({ ...value, speed: on ? "1" : "" })}
                />
                <FieldLabel htmlFor={`${idPrefix}-speed-on`} className="font-normal">
                  {t.overrideSpeed}
                </FieldLabel>
              </Field>
              {value.speed && engine ? (
                <div className="grid gap-1.5">
                  <div className="flex justify-end text-sm text-muted-foreground tabular">
                    {m.voice.speedValue(formatDecimal(speed))}
                  </div>
                  <Slider
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
                  <p className="text-xs text-muted-foreground">
                    {m.voice.speedHint(formatDecimal(engine.minSpeed), formatDecimal(engine.maxSpeed))}
                  </p>
                </div>
              ) : null}
            </div>
          </fieldset>
          <p className="mt-4 text-sm">
            <Link
              to="/w/$workspaceId/settings"
              params={{ workspaceId: workspace.id }}
              hash="voice"
              className="font-medium underline-offset-4 hover:underline"
            >
              {t.settingsLink}
            </Link>
          </p>
        </CardContent>
      ) : null}
    </Card>
  )
}
