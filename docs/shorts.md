# Shorts from long videos — plan

A long video the workers made can be cut into vertical shorts. Each short is a derived
deliverable of the long video's story, re-composed for 9:16 around a slice of the long
video's narration, and reviewed and published like any other video. Uploading outside
videos is out of scope for now.

## 1. Where we are

| Today | Effect |
|---|---|
| A long video's revision holds `script.md`, `scenes.json`, `narration/speech.wav` and `narration/timings.json` (words and segments with times) | The transcript is free: no speech-to-text is needed to find or cut a moment. |
| Its work dir holds `composition.html`, `assets/` (stock clips, photos) with `credits.json`, and the music | A short can reuse the visuals and credits without searching Pexels again. |
| A story has at most one derived deliverable, released when its lead is live (`ReleaseDerived`) | Shorts follow the same rule, so their descriptions can link to the published long video. |
| `prepare` writes the work dir from the brief; the agent runs `kurgu tool tts`, the check reads `narration/timings.json`, the render mixes `narration/speech.wav` with the music | If `prepare` writes the slice as `narration/speech.wav` and `narration/timings.json`, the composition contract, captions, check and `Mix` work unchanged; the agent only must not narrate. |

## 2. Limits

Read from the environment at start, parsed once into `story.ShortsLimits` and passed to
the domain (the domain never reads the environment). Bad values fail the start.

| Env | Default | Meaning |
|---|---|---|
| `KURGU_SHORTS_SOURCE_MIN` | `4m` | a long video shorter than this (rendered length, `Measures.Duration`) is not cut |
| `KURGU_SHORTS_SOURCE_MAX` | `60m` | nor one longer than this |
| `KURGU_SHORTS_CLIP_MIN` | `20s` | shortest short; at least `MinShortDuration` (10 s) |
| `KURGU_SHORTS_CLIP_MAX` | `60s` | longest short; at most `MaxShortDuration` (3 min) |
| `KURGU_SHORTS_MAX_PER_VIDEO` | `5` | shorts one long video may have, automatic and manual together |
| `KURGU_SHORTS_MIN_GAP` | `30s` | moments of one video may not overlap or sit closer than this |

Each minimum must not exceed its maximum, and the clip minimum must fit in the source
minimum.

## 3. Model — in the `story` context

**Clip** (value): `start`, `end` in seconds of the source's narration. A short
deliverable carries one (`d.Clip()`); nil for every other deliverable.

**Moment** (owned by the story, kept per source deliverable):

| Field | Notes |
|---|---|
| id, source deliverable | UUIDv7; the long video it was found in |
| clip | start and end, snapped to narration segments |
| hook | the short's working title, what grabs in the first seconds |
| reason | why it stands alone, for the person choosing |
| score | 0–100, the agent's ranking; 0 for manual moments |
| origin | `agent` · `manual` |
| status | `suggested` → `made` (with the short's deliverable id) · `discarded` |

Domain rules:

- `CanCut(source, limits)` — the source is a long video, live (published, or ready with
  no publisher), and its rendered length is within the source limits.
- `SuggestMoments(source, candidates, segments, limits)` — snaps each candidate's start
  to the segment it falls in and its end to the end of the segment it falls in (TTS
  pauses between sentences, so the cut is clean), drops those outside the clip limits,
  then keeps candidates by score, dropping any that overlap or come closer than the gap
  to one already kept or made. Replaces earlier `suggested` moments of that source.
- `AddMoment(source, clip, segments, limits)` — a manual moment, same snapping and checks.
- `AdjustMoment(id, clip, …)`, `DiscardMoment(id)`.
- `AddShorts(source, momentIDs, limits, ids, now)` — one vertical short per moment
  (role `derived`, source = the long video, duration = the clip's length, the clip
  attached), queued; marks each moment `made`. Refused past `MaxPerVideo` shorts of that
  source.

**Pipeline**: a long video's `VideoSpec` gains `shortsPerVideo` (0–20). It is clamped to
`MaxPerVideo` when the shorts are made, so lowering the limit later is safe.

**Persistence**: `deliverables` gains `clip_start_ms`, `clip_end_ms`; a new
`story_moments` table (workspace, story, source deliverable, clip, hook, reason, score,
origin, status, short id, created at), saved with the story like its deliverables.

## 4. Flow

1. **Live.** When a long video becomes live and its video spec asks for shorts,
   `story.moments` is enqueued (control queue, unique per deliverable) in the same
   transaction. A video outside the source limits is left alone (logged).
2. **Scan.** The agent reads the long video's `script.md`, `scenes.json` and
   `narration/timings.json` and returns candidates as typed JSON (start, end, hook,
   reason, score), asked for twice the wanted count. Go runs `SuggestMoments`, then
   `AddShorts` with the top N.
3. **By hand.** On the long video's page, *Find moments* runs the same scan without
   making shorts; the person adjusts, discards or adds moments (selecting transcript
   text) and presses *Make shorts*.
4. **Draft (clip mode).** `prepare` for a deliverable with a clip writes (pure Go, no
   ffmpeg on the server: `pkg/wav` copies the span's PCM frames with 15 ms fades):
   - `narration/speech.wav`, `narration/timings.json`, `narration/narration.txt` — the
     slice of the long video's narration, times shifted to 0, engine and voice kept;
   - `clip/window.wav`, `clip/window.json` — the long narration ±15 s around the clip;
     `clip/clip.json` — where the clip sits in the window and the long video, the short
     limits, and the earliest and latest cut that keeps the gap to the other shorts;
   - on the first draft, `assets/` with `credits.json`, `music.js` / `music.json` —
     copied from the long video's revision, so the score and stock come along (the
     render makes `music.wav` for the short's length as for any video);
   - `sources/lead.md` — the long video's script and metadata;
   - `sources/long/composition.html` and `scenes.json` — how the long video looked
     (the agent reads the code rather than stills of the render, which would mean
     downloading the final video on the server);
   - `sources/clip.md` — the window's sentences, numbered, those in the short marked ▶;
   - `INSTRUCTIONS.md` from `prompts/clip.md` — the hook in the first two seconds, word
     captions, a 9:16 layout, an end card pointing to the full video.
5. **Rules** (in the tools and `validate`, not only the prompt): `kurgu tool tts` is
   refused in clip mode; the check fails when the narration is not the clip's length
   and when the composition does not cover it (plus at most 4 s). `kurgu tool clip
   --first N --last M` moves the cut to window sentences N–M within the short limits and
   the gap, rewriting `narration/`; `FinishDraft` records the new clip on the short and
   its moment (`Story.Recut`). A reviewer cannot edit a short's narration text.
6. **Review and publishing** as today; the metadata links to the long video.

## 5. API

| Operation | Access |
|---|---|
| `POST /workspaces/{ws}/stories/{id}/deliverables/{did}/moments/scan` (phase 3) | edit |
| `GET /workspaces/{ws}/stories/{id}/deliverables/{did}/moments` (moments, sentences, limits, `blocked`) | view |
| `POST /workspaces/{ws}/stories/{id}/deliverables/{did}/moments` (manual) | edit |
| `PATCH · DELETE /workspaces/{ws}/stories/{id}/moments/{mid}` | edit |
| `POST /workspaces/{ws}/stories/{id}/deliverables/{did}/shorts` `{momentIds}` | edit |

Deliverables gain `clip {startSeconds, endSeconds}`; a video spec gains `shortsPerVideo`.
The moments listing carries the shorts limits, so the UI can bound its inputs.

## 6. Web UI

- **Pipeline and request forms** → with a long video: "Shorts per video" (0–20, sent as
  `outputs.video.shortsPerVideo`); the pipeline summary says "· 3 shorts cut from each".
- **Story page** → a short is a deliverable of the long video's story, labelled
  "Short · 0:51–1:11" in the deliverable switcher.
- **Shorts section** of the review panel, on a long video that is ready or published:
  how many shorts are made of the limit; *Find moments* (follows the scan job); the
  moments with their range (click to play the long video there), length, score or
  "Yours", hook and reason; for a suggested one, a checkbox, start / end one sentence
  earlier or later, and *Set aside*; a made one links to its short; *Make N shorts*
  for the checked ones within the room left; *Choose sentences* lists the narration to
  pick a moment's first and last sentence (checked against the short limits) and add
  it. When the video cannot be cut, the section says why (`blocked`).

## 7. Phases

Each phase ships on its own with tests (domain table tests, repository conformance,
service with inmemory repos and a fixed clock, `httptest` through the router).

0. **A real long video.** Long-form is not yet produced end to end (PLAN §8.9); phase 2
   needs at least one approved long video to try against.
1. ✅ **Domain and limits** — `ShortsLimits` from env, `Clip`, `Moment`, the rules above,
   migration 00027, repository conformance, moments service.
2. ✅ **Clip-mode drafting** — slicing in `prepare`, `prompts/clip.md`, `kurgu tool clip`,
   enforcement, and the moments and shorts endpoints with manual ranges. Usable end to
   end from here through the API (not yet run against a real long video).
3. ✅ **Scan and moments panel** — the `story.moments` job (control queue: one typed
   agent call, like idea planning, so remote workers need nothing new; the agent
   answers by sentence number, so moments always fall on sentences), the scan
   endpoint, and the Shorts section of the review panel.
4. ✅ **Pipeline automation** — `VideoSpec.shortsPerVideo` (stored inside the video
   spec, so the long deliverable carries it and ad-hoc requests get it too); when the
   long video goes live, `release` queues one scan (unique per video) that makes the
   best N into shorts; the form field.

When phase 2 lands, add shorts from long videos to PLAN.md §1 and §6.

## 8. Decisions

- **Re-compose, not crop.** A long video's text and graphics are laid out for 16:9;
  cropping cuts them. The short keeps the narration and assets and gets its own layout.
- **Trigger:** both a pipeline setting (automatic) and a manual action.
- **Timing:** once the long video is live, like other derived deliverables.
- **Same story.** Shorts are deliverables of the long video's story, so review and
  history stay together and dedupe does not apply.
