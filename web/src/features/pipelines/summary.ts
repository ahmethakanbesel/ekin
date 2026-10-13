import type { PipelineDefinition } from "@/api/client"
import { m } from "@/i18n"
import { formatDuration } from "@/lib/time"

export type OutputLine = { kind: "video" | "article"; text: string; lead: boolean }

/** One line per produced deliverable, lead marked when there are two. */
export function outputLines(def: PipelineDefinition): OutputLine[] {
  const { video, article, lead } = def.outputs
  const both = !!video && !!article
  const lines: OutputLine[] = []
  if (video) {
    lines.push({
      kind: "video",
      text: m.pipelines.videoSummary(
        m.values.format[video.format] ?? video.format,
        m.values.orientation[video.orientation] ?? video.orientation,
        formatDuration(video.durationSeconds),
      ),
      lead: both && lead === "video",
    })
  }
  if (article) {
    lines.push({
      kind: "article",
      text: m.pipelines.articleSummary(
        article.words,
        article.translations.map((l) => l.toUpperCase()).join(", "),
      ),
      lead: both && lead === "article",
    })
  }
  return lines
}
