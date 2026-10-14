import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import {
  ApiError,
  api,
  call,
  type Language,
  type VoiceConfig,
  type VoiceSettings,
} from "@/api/client"
import { keys } from "./keys"

/** The TTS engine of each language: voices and limits. The same for every workspace. */
export const voicesQuery = () =>
  queryOptions({
    queryKey: keys.voices(),
    queryFn: async ({ signal }) => {
      const res = await call(api.GET("/voices", { signal }))
      return res.engines
    },
    staleTime: Infinity,
  })

export const voiceSettingsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.voiceSettings(workspaceId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/voice", { params: { path: { workspaceId } }, signal })
      ),
  })

export function useSaveVoiceSettings(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: VoiceSettings) =>
      call(api.PUT("/workspaces/{workspaceId}/voice", { params: { path: { workspaceId } }, body })),
    onSuccess: (saved) => qc.setQueryData(keys.voiceSettings(workspaceId), saved),
  })
}

const PREVIEW_POLL_MS = 1_500
const PREVIEW_TIMEOUT_MS = 5 * 60_000

export type PreviewInput = {
  language: Language
  config: VoiceConfig
  text?: string
  signal?: AbortSignal
}

function wait(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms)
    signal?.addEventListener("abort", () => {
      window.clearTimeout(timer)
      reject(new DOMException("Preview cancelled", "AbortError"))
    })
  })
}

/**
 * Renders a sample on a worker: queues the preview, waits for its job and
 * resolves to the WAV audio.
 */
export function usePreviewVoice(workspaceId: string) {
  return useMutation({
    mutationFn: async ({ language, config, text, signal }: PreviewInput): Promise<Blob> => {
      const queued = await call(
        api.POST("/workspaces/{workspaceId}/voice/previews", {
          params: { path: { workspaceId } },
          body: { language, config, text: text || undefined },
        })
      )
      const deadline = Date.now() + PREVIEW_TIMEOUT_MS
      let job = queued.job
      while (job.status === "queued" || job.status === "running") {
        if (Date.now() > deadline) throw new ApiError(0, "PreviewTimeout", "")
        await wait(PREVIEW_POLL_MS, signal)
        job = await call(
          api.GET("/workspaces/{workspaceId}/jobs/{jobId}", {
            params: { path: { workspaceId, jobId: job.id } },
            signal,
          })
        )
      }
      if (job.status !== "succeeded") {
        throw new ApiError(0, "PreviewFailed", job.lastError ?? "")
      }
      return call(
        api.GET("/workspaces/{workspaceId}/voice/previews/{previewId}", {
          params: { path: { workspaceId, previewId: queued.previewId } },
          parseAs: "blob",
          signal,
        })
      ) as Promise<Blob>
    },
  })
}
