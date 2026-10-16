import { useQuery } from "@tanstack/react-query"

import type { ElevenLabsAccount, Narrator, VoiceEngine } from "@/api/client"
import { elevenLabsAccountQuery, voicesQuery } from "@/queries/voice"

/**
 * Who read a narration: "EMA Lightning", "Kokoro-82M · George (British
 * male)", "ElevenLabs · George · Eleven Multilingual v2". An engine's
 * only voice goes unnamed; an ElevenLabs voice the account no longer
 * lists is left out rather than shown as an ID, and so is a voice not
 * recorded (revisions narrated before Kurgu kept it).
 */
export function narratorName(
  n: Narrator,
  language: string,
  engines: VoiceEngine[] | undefined,
  account: ElevenLabsAccount | undefined
): string {
  const engine = engines?.find(
    (e) => e.id === n.engine && e.language === language
  )
  const parts = [engine?.name ?? n.engine]
  const voiceId = n.voice
  if (n.engine === "elevenlabs") {
    const voice = account?.voices.find((v) => v.id === voiceId)
    if (voice) parts.push(voice.name)
    if (n.model)
      parts.push(account?.models.find((x) => x.id === n.model)?.name ?? n.model)
  } else if (engine && engine.voices.length > 1 && voiceId) {
    parts.push(engine.voices.find((v) => v.id === voiceId)?.label ?? voiceId)
  }
  return parts.join(" · ")
}

/**
 * Names narrators in a workspace. The ElevenLabs account (which calls
 * ElevenLabs) is fetched only when one of them is an ElevenLabs voice.
 */
export function useNarratorNames(
  workspaceId: string,
  narrators: (Narrator | undefined)[]
) {
  const any = narrators.some(Boolean)
  const engines = useQuery({ ...voicesQuery(), enabled: any })
  const account = useQuery({
    ...elevenLabsAccountQuery(workspaceId),
    enabled: narrators.some((n) => n?.engine === "elevenlabs"),
  })
  return (n: Narrator | undefined, language: string): string | undefined =>
    n ? narratorName(n, language, engines.data, account.data) : undefined
}
