import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { fromProduction, type ProductionValues } from "@/features/pipelines/model"
import { VoiceOverrideRows } from "@/features/voice/override-section"
import type { VoiceOverrideValues } from "@/features/voice/model"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { workspaceProductionQuery } from "@/queries/production"
import { LayeredCard } from "./layered"
import { ProductionFields, countSet, layerResolved, serverResolved } from "./production-fields"

/**
 * "Production" in the pipeline and request forms: every setting beside
 * what the workspace gives, unset unless this pipeline or request
 * changes it. Voice rows follow the content language.
 */
export function ProductionCard({
  layer,
  idPrefix,
  value,
  onChange,
  voice,
  onVoiceChange,
  language,
  video,
  longVideo,
  readOnly = false,
}: {
  layer: "pipeline" | "request"
  idPrefix: string
  value: ProductionValues
  onChange: (next: ProductionValues) => void
  voice: VoiceOverrideValues
  onVoiceChange: (next: VoiceOverrideValues) => void
  language: string
  video: boolean
  longVideo: boolean
  readOnly?: boolean
}) {
  const workspace = useWorkspace()
  const defaults = useQuery(workspaceProductionQuery(workspace.id))
  const t = m.settings.production
  const here = layer === "pipeline" ? t.thisPipeline : t.thisRequest
  const { set } = countSet(value, { video, longVideo })
  const voiceSet = [voice.engine, voice.voice, voice.speed].filter(Boolean).length
  const summary =
    set + voiceSet === 0 ? t.allInherited(workspace.name) : t.changed(set + voiceSet, here.toLowerCase())
  // A request starts folded: most take every workspace default.
  const [open, setOpen] = useState(layer === "pipeline" || set + voiceSet > 0)

  if (!open)
    return (
      <Card>
        <CardHeader className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <CardTitle>{t.title}</CardTitle>
            <CardDescription>{summary}</CardDescription>
          </div>
          <Button type="button" variant="outline" onClick={() => setOpen(true)}>
            {t.change}
          </Button>
        </CardHeader>
      </Card>
    )

  let body
  if (defaults.isPending)
    body = (
      <div className="grid gap-3 px-6 py-4" aria-busy="true" aria-label={m.common.loading}>
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    )
  else if (defaults.isError)
    body = (
      <div className="px-6 py-4">
        <ErrorState error={defaults.error} onRetry={() => void defaults.refetch()} />
      </div>
    )
  else {
    const inherited = layerResolved(
      fromProduction(defaults.data.defaults, defaults.data.selfReviewRounds),
      serverResolved(defaults.data.server),
      workspace.name
    )
    body = (
      <>
        <ProductionFields
          idPrefix={idPrefix}
          value={value}
          onChange={onChange}
          inherited={inherited}
          here={here}
          video={video}
          longVideo={longVideo}
          readOnly={readOnly}
        />
        {video ? (
          <VoiceOverrideRows
            idPrefix={`${idPrefix}-voice`}
            language={language}
            value={voice}
            onChange={onVoiceChange}
            readOnly={readOnly}
          />
        ) : null}
      </>
    )
  }

  return (
    <LayeredCard
      title={t.title}
      description={
        <>
          {layer === "pipeline" ? t.pipelineDescription : t.requestDescription}{" "}
          <Link
            to="/w/$workspaceId/settings/production"
            params={{ workspaceId: workspace.id }}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            {t.workspaceLink}
          </Link>
        </>
      }
      summary={summary}
      fromLabel={m.settings.columns.inherited}
      hereLabel={here}
    >
      <div className="pb-4">{body}</div>
    </LayeredCard>
  )
}
