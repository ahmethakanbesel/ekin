# Kurgu — product and architecture plan

Kurgu turns ideas, knowledge-base documents and SEO opportunities into YouTube videos
and blog articles, renders them unattended, and holds every piece for human review
before anything is published. First user: acme. Turkish first, English second.

Update this file whenever a decision below changes.

## 1. Scope

- **Pipelines** produce content on a cron schedule or on demand.
  - Output is a video, an article, or both. Both is optional, never required.
  - When a pipeline produces both, one is the **lead** and the other is derived from it
    (video → article that embeds and expands it; article → one or more shorts cut from it).
  - Language per pipeline: `tr` or `en`. Articles may be translated (tr → en) for parity.
  - Source strategy per pipeline: a **prompt** (system-prompt-like brief), the
    **knowledge base** (referenced documents and page ranges), or **SEO opportunities**
    (OpenSEO, later).
  - Schedule: cron expression + IANA timezone + how many items per run. Manual-only
    pipelines have no schedule.
- **Ad-hoc jobs**: a structured form (kind, count, duration, orientation, language,
  style, knowledge refs) plus a free-text idea. Runs in the background, emails on finish.
- **Review**: every deliverable waits in review. Feedback (optionally timestamped and
  scoped to script / voice / visuals / metadata) sends it back to the agent for another
  revision. Approve or reject per deliverable.
- **Publishing**: YouTube (private upload → public on approval, after the API audit
  passes) and the marketing site's Astro content collections via git commit.
- **Knowledge base**: upload PDF / TXT / MD / EPUB, extract text per page, estimate tokens
  per page, preview in the browser, reference several documents and page ranges at once.
  Only the referenced pages are given to the agent. No RAG / indexing for now.
- **Deduplication**: every idea is compared against previous stories before production.
- **Multi-user ready**: everything is scoped to a workspace; users have roles.

## 2. Decisions

| Topic | Decision |
|---|---|
| Orchestration | Go backend. No n8n. |
| Database | SQLite (`modernc.org/sqlite`, CGO-free), WAL, one writer connection. Litestream to S3 for backup. |
| Job queue | Own small queue on SQLite behind a `Queue` port (lease, heartbeat, retry, unique keys). |
| Remote workers | Talk to the API (lease / heartbeat / complete, presigned blob uploads) — never to the database file. |
| Blob storage | `BlobStore` port: `fs` driver (Mac / dev), `s3` driver (VPS: R2 or Hetzner). |
| API | OpenAPI 3.1 spec-first, `oapi-codegen` strict server on `net/http` ServeMux, typed TS client. |
| Web UI | Bun, React, TanStack (Router, Query, Form, Table), shadcn on Base UI, oxc (oxlint), tsgo. Static build embedded in the Go binary. |
| Authoring agent | Claude Code headless (`claude -p`) behind an `Author` port, restricted tools, Kurgu MCP server. Swappable for API key / Agent SDK. |
| Decision model | OpenRouter `typesafe/jev-1.13` for typed decisions only: dedupe verdict, idea selection, compliance check. |
| Render engine | Chosen by benchmark: vanilla (`renderAt(t)` + Playwright + ffmpeg) vs Remotion vs Revideo. |
| TTS | `Speech` port. Turkish: okur-tts (with word timings from its duration predictor). English: Kokoro-82M (timestamped ONNX). |
| Music | `Music` port. MVP: procedural — the agent writes a Tone.js score rendered offline in Chromium. Paid API (Lyria) may follow. |
| Stock media | Pexels video and photos (no audio). |
| Blog | MDX committed to `marketing/src/content/blog/{tr,en}` via the GitHub API. No CMS migration (EmDash revisited later). |
| Notifications | Web UI + SMTP email. |
| Deployment | Dokploy, Compose type (needs `cpuset` / `cpu_shares`). Also runs natively on macOS. |

## 3. Runtime topology

```
kurgu serve    API + embedded web UI + scheduler tick
kurgu worker   job runner: agent, tts, render, extract, io queues
kurgu migrate  schema migrations
kurgu mcp      stdio MCP server given to Claude Code (per job)
```

- **VPS**: `serve` and `worker` containers, SQLite on a volume, Litestream sidecar.
- **Mac**: native `serve` + `worker` (Chromium, VideoToolbox, MLX and the Claude login work
  without containers), `fs` blob driver.
- **Hybrid**: a Mac worker leases heavy jobs from the VPS API over Tailscale; the VPS sets
  those queues' concurrency to 0.

## 4. Throttling (no time windows)

Runtime-editable settings, with env defaults:

| Setting | VPS default | Purpose |
|---|---|---|
| `queue.<name>.concurrency` | agent 1, render 1, tts 1, extract 1, io 2 | 0 disables a queue on that host |
| `heavy.exclusive` | true | agent / render / tts never overlap |
| `heavy.cooldown` | 30s | pause between heavy jobs |
| `render.duty_cycle` | 1.0 | sleep between frames (0.25 = 4× slower) |
| `render.preview_scale` / `render.final_scale` | 0.5 / 1.0 | review at 540p, final at 1080p after approval |
| `render.fps`, `render.chunks`, `render.encoder`, `render.gpu` | 30, 1, libx264, off | Mac: chunks = cores, h264_videotoolbox, gpu on |
| `ffmpeg.threads`, `process.nice` | 1, 19 | |
| `worker.paused` | false | global pause |

Container ceiling on the VPS: `cpuset: "1"`, `cpu_shares: 64`, `cpus: 0.5`,
`mem_limit` = `memswap_limit` = 1.5g, `blkio_weight: 100`.

## 5. Bounded contexts

| Context | Aggregates |
|---|---|
| identity | User, Session, Membership (role), Invitation |
| workspace | Workspace (timezone) |
| pipeline | Pipeline (language, outputs, source, schedule, status) |
| knowledge | Document, Page, Ref (document + page ranges), Coverage |
| ideation | Idea (fingerprint), Opportunity |
| story | Story (idea, brief, sources) owning deliverables: Video, Article, Translation; Revision; Feedback |
| publishing | ChannelConnection, BlogTarget, Publication |
| job | Job (queue, kind, unique key, attempts, lease) |
| settings | Throttle and host profiles |
| notify | Email digests |

Derived deliverables record the source revision they came from; changing the source marks
them **stale** with a one-click re-derive.

## 6. Production flow

1. Pick idea (source strategy) → dedupe (FTS5 trigram candidates + Jev verdict) → brief.
2. Seed a working directory: brief, referenced pages only, style tokens, tested animation
   components, the composition contract.
3. Claude Code writes the script → `tts` tool (word timings) → `composition.html` +
   `scenes.json` + `music.js`, checking its work with `preview_frame(t)`.
4. Validate (schema, duration vs audio, no network during render) → preview render (540p).
5. Review → feedback → `claude --resume` on the affected stages only → new revision.
6. Approve → final render (1080p) → publish → cross-link video ↔ article.

Long-form video is the same flow per chapter, concatenated with ffmpeg.

## 7. External prerequisites

- **YouTube Data API audit**: until it passes, API uploads are locked private and cannot
  be made public. Apply on day one. Move the OAuth consent screen to "In production"
  (testing-mode refresh tokens expire after 7 days). Check upload quota in Cloud Console.
- **Claude Code on the server**: `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN`.
  Subscription use for headless runs is a moving policy; keep the `Author` port swappable.

## 8. Milestones

0. Spikes: YouTube audit submitted; okur `--timings`; `claude -p` on the VPS; render
   engine benchmark (5 fixed briefs × 3 engines, VPS profile at cpus 1.0 / 0.5 / 0.25 and
   Mac chunked + VideoToolbox).
1. Skeleton: layered Go module with architecture lint, SQLite + migrations, job queue,
   scheduler, pipelines API, embedded web shell, `make dev` on the Mac.
2. Identity: users, workspaces, sessions, roles.
3. Knowledge base: upload, extraction, viewer, refs, token estimates.
4. Shorts MVP: prompt-source pipeline, ad-hoc form, preview render, review loop, final render.
5. Dedupe, SMTP notifications, settings UI, hybrid worker API.
6. YouTube publishing.
7. Articles: Turkish article, English translation, git publish, video ↔ article derivation.
8. OpenSEO opportunities as a source strategy.
9. Long-form video (16:9, chapters).
