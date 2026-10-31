# Ekin — product and architecture plan

Ekin is a self-hosted growth agent for a team's website and channels. It turns ideas,
knowledge-base documents and SEO opportunities into YouTube videos and blog articles,
audits the site and proposes fixes to its code as pull requests, tracks whether AI
answer engines cite it, and measures what each published change did. Everything runs
unattended and every piece waits for human review before it goes out. For small teams
anywhere; content in English or Turkish.

Update this file whenever a decision below changes.

## 1. Scope

- **Pipelines** produce content on a cron schedule or on demand.
  - Output is a video, an article, or both. Both is optional, never required.
  - When a pipeline produces both, one is the **lead** and the other is derived from it
    (video → article that embeds and expands it; article → one or more shorts cut from it).
  - Language per pipeline: `en` (the default) or `tr`. Articles may be translated into the
    other language for parity.
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
- **Shorts from long videos**: once a long video is live, moments of it (spans of whole
  sentences, found by the agent or chosen by a person) become vertical shorts that speak
  that slice of its narration with a new 9:16 composition (docs/shorts.md). Limits come
  from `EKIN_SHORTS_*`.
- **Knowledge base**: upload PDF / TXT / MD / EPUB, extract text per page, estimate tokens
  per page, preview in the browser, reference several documents and page ranges at once.
  Only the referenced pages are given to the agent. No RAG / indexing for now.
- **Media library**: music, stock videos and photos kept per workspace for reuse
  (docs/library.md). Uploaded files are processed with ffmpeg; each pipeline or request
  picks, per kind, whether drafts compose and search fresh, use the library only, or
  look there first. Usage is counted, never limited. Optionally (per kind, off by default)
  drafts' Pexels stock and composed scores are saved as they finish; once the video is
  decided only what its last revision used is kept.
- **Thumbnails**: a long video gets 1–4 designed thumbnail variants (3 by default) drawn
  on a page of their own; the reviewer picks one, rewords them in place or sends notes
  on them alone, none of which renders the video again (docs/thumbnails.md). Shorts
  keep a frame of the video.
- **Captions**: every narrated revision offers its narration as SRT and WebVTT, made on
  request from the word timings.
- **Team notes**: one note per revision, written and edited by people, never read by
  the agent.
- **Deduplication**: every idea is compared against previous stories before production.
- **Multi-user ready**: everything is scoped to a workspace; users have roles.

### Site growth (milestones 10–15)

The site is the workspace's website as a git repository plus its public URL. Ekin
never hosts it and never migrates it into a CMS: it reads the live site, edits the
repository, and changes reach the site only through a commit or a pull request that a
person approved.

- **Site**: one per workspace: its address, a page limit per crawl (500 by default, at
  most 5000) and a crawl schedule (weekly by default, daily, or only on demand). A new
  site, or one moved to another address, is crawled at once; a move drops the old
  pages and findings. The repository settings stay on the blog target until changes
  need them (milestone 12), when they join the site with a build command; competitors'
  domains join it in milestone 15.
- **Crawl and inventory**: robots.txt and the sitemaps (falling back to following
  internal links, capped) give the page inventory: URL, status, redirect target,
  title, meta description, canonical, `h1`, structured-data types, word count, internal
  links in and out, first and last seen. Plain HTTP fetches; headless Chrome only for a
  site that renders client-side, opted into per site.
- **Audit**: deterministic rules in Go turn the inventory into **findings** (rule,
  severity, page, evidence), each with a stable key so a rerun updates, reopens or
  resolves it rather than duplicating it. Rules cover status and redirect chains,
  missing or duplicate titles and descriptions, canonicals, `h1`, broken internal links,
  orphan pages, sitemap drift, structured data, `llms.txt`, and whether robots.txt lets
  AI crawlers (GPTBot, ClaudeBot, PerplexityBot, Google-Extended, …) in. Core Web Vitals
  come from the PageSpeed Insights API when a key is set.
- **Changes**: a third deliverable kind, `change`, next to `video` and `article`. Its
  source is a set of findings, an SEO opportunity that an existing page should cover,
  or a prompt ("add a comparison page against X"). The agent works in a shallow checkout
  of the repository, writes the edit, runs the site's build through `ekin tool build`,
  and leaves a diff and a summary saying which findings it addresses. Review shows the
  diff (and the host's preview deployment when the PR has one); feedback resumes the
  session as for any deliverable; approval publishes it through the site's publish mode.
- **Pull requests and merge tracking**: in pull-request mode, publishing an article or
  a change pushes a branch (`ekin/<slug>`) and opens a PR. The deliverable then waits in
  **merging** while the server checks the PR every few minutes: merged → **published** at the live URL;
  closed without merging → **failed** with that reason (Retry opens a fresh PR).
- **Measurement**: Google Search Console (OAuth, read-only) is pulled daily as clicks,
  impressions and average position per page and per query. Each published article or
  change shows its page's 28 days before against the days since, and the digest says
  what moved. Ekin keeps those daily aggregates only; it never collects raw visitor
  events itself.
- **AI visibility**: a workspace keeps a set of prompts its buyers would ask
  (drafted by the agent from the SEO profile and competitors, edited by people). A
  scheduled run asks each configured model through OpenRouter and records whether the
  brand is mentioned, where, which URLs are cited and which competitors appear. Every
  run states its call count before it starts and a monthly cap stops it.
- **Competitors**: a scheduled sitemap diff of each competitor's site records new and
  removed pages; new pages become signals for SEO opportunities and change ideas.
- **Pipelines as playbooks**: pipelines gain `findings` (fix the most severe open
  findings, N per run) and `visibility` (cover prompts where the brand is missing) as
  source strategies, and `change` as an output.

Not in scope yet: visitor identification, outreach, CRM sync, ads, A/B tests, hosting.

## 2. Decisions

| Topic | Decision |
|---|---|
| Orchestration | Go backend. No n8n. |
| Database | SQLite (`modernc.org/sqlite`, CGO-free), WAL, one writer connection. Litestream to S3 for backup. |
| Job queue | Own small queue on SQLite behind a `Queue` port (lease, heartbeat, retry, unique keys). |
| Remote workers | `ekin worker` with `EKIN_REMOTE_URL` + `EKIN_WORKER_TOKEN` claims agent and render jobs over the worker API, downloads the job's work dir, runs it locally and uploads the result — never touching the database or blob store. |
| Blob storage | `BlobStore` port: `fs` driver (Mac / dev), `s3` driver (VPS: R2 or Hetzner). |
| API | OpenAPI 3.1 spec-first, `oapi-codegen` strict server on `net/http` ServeMux, typed TS client. |
| Web UI | Bun, React, TanStack (Router, Query, Form, Table), shadcn on Base UI, oxc (oxlint), tsgo. Static build embedded in the Go binary. |
| Authoring agent | Claude Code headless (`claude -p`) behind an `Agent` port with an explicit tool allow-list: file tools plus `Bash(ekin tool:*)`. The agent's tools (`tts`, `frame`, `check`, `stock`, `music`) are `ekin tool` subcommands rather than an MCP server — simpler, and the same binary on every host. Host settings, plugins and MCP servers are off except for the agent-based OpenSEO refresh (used only without an API key). |
| Decision model | OpenRouter `typesafe/jev-1.13` for typed decisions only: the duplicate-idea verdict for titles that are similar but not identical (trigram score 0.35–0.6). Ideas themselves come from Claude Code with a JSON schema. |
| Render engine | **Vanilla** (docs/benchmarks/render.md): `composition.html` + `window.ekin.renderAt(t)` driven by Go chromedp, frames piped to ffmpeg. `HeadlessExperimental.beginFrame` in chrome-headless-shell on Linux (3–13× faster on 0.5–1 vCPU), screenshots on macOS. Remotion: licence cost; Revideo: canvas-only, broken with ffmpeg 8. |
| TTS | `Speech` port. English: Kokoro-82M (timestamped ONNX). Turkish: okur-tts (with word timings from its duration predictor). |
| Music | `Music` port. MVP: procedural — the agent writes a Tone.js score rendered offline in Chromium. Paid API (Lyria) may follow. Or a library track (`music.json`), cut to length and faded out. |
| Stock media | Pexels video and photos (no audio); clips are transcoded to short VP8 WebM so the composition can seek them frame by frame. Or library items, through `ekin tool library`. |
| Blog | Markdown with the site's frontmatter committed to `marketing/src/content/blog/{tr,en}` in one commit through the GitHub Git Data API — onto the branch, or onto `ekin/<slug>-<job>` with a pull request against it — or written into a local checkout. Video embeds become privacy-enhanced iframes. No CMS migration. |
| Merge tracking | `ekin serve` calls `CheckMerges` every `EKIN_MERGE_POLL` (5m): one GET per open pull request, no jobs. Not a job chain, so days of waiting leave no trail of jobs. The branch name ends in the publish job's id, so a retried attempt finds its own pull request and a new publication gets a fresh branch. |
| Site crawl | `internal/adapter/crawl`: Go `net/http`, one request at a time with `EKIN_CRAWL_DELAY` (500ms) between them or a longer Crawl-delay (up to 10s); robots.txt obeyed as `EkinBot` (`pkg/robots`, RFC 9309); redirects recorded, not followed; `golang.org/x/net/html` for parsing. The dialer refuses loopback, private and link-local addresses after DNS resolution unless `EKIN_CRAWL_ALLOW_PRIVATE`. A `site.crawl` job on the io queue, one per workspace at a time; a crawl unfinished after 3h running (6h queued) is taken as stopped. The latest crawl's pages replace the inventory (first seen kept); page bodies are not kept. |
| Audit rules | Plain Go functions over the inventory, table-tested; no model decides whether a title is missing. The agent only writes fixes. |
| Change agent | Same `Agent` port. Work dir holds a shallow clone (`git clone --depth 1`) of the base branch; allow-list adds `Bash(ekin tool build)`, `Bash(ekin tool diff)` and nothing else from the shell. The build command comes from the site settings and runs with the job's CPU limit and no secrets in its environment. |
| Git publishing | GitHub Git Data API as today; pull-request mode adds a branch ref and `POST /pulls`. A `git` adapter for other hosts may follow. |
| Search Console | The Google OAuth client already used for YouTube, connected separately with the `webmasters.readonly` scope; the refresh token is sealed like the channel's. |
| AI visibility models | OpenRouter (already a credential), so one key reaches several answer engines' models. Answers are kept for 90 days; mentions and citations for good. |
| Notifications | In-app feed + SMTP digests (one email per person per flush, `EKIN_NOTIFY_EVERY`). Invitations are emailed when SMTP is set. |
| Deployment | Dokploy, Compose type (needs `cpuset` / `cpu_shares`). Also runs natively on macOS. |

## 3. Runtime topology

```
ekin serve    API + embedded web UI + scheduler tick
ekin worker   job runner: agent, tts, render, extract, io queues
ekin migrate  schema migrations
ekin mcp      stdio MCP server given to Claude Code (per job)
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
| `render.preview_scale` / `render.final_scale` | 0.5 / 1.0 | review at 540p, final at 1080p after approval (up to 2.0: 1440p · 2K or 2160p · 4K, with stock fetched and kept at that size); a preview that covers the final (as sharp, same frame rate) is reused instead. Mac: preview 1.0, since screenshot capture costs the same at 540p |
| `render.preview_fps` / `render.final_fps` | 30 / 30 | preview 15, 24 or 30; final 30 or 60. A workspace, then a pipeline or ad-hoc request, can override these and the scales (see Settings layers) |
| `render.chunks`, `render.encoder`, `render.gpu` | 1, libx264, off | Mac: chunks = cores, h264_videotoolbox, gpu on |
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
| library | Asset (music, video, photo; uploaded, Pexels or composed), Usage, media source Policy |
| seo | Opportunity (keyword, volume, difficulty, intent), SEO profile |
| voice | Voice settings per language (engine voice, speed, pauses, seed, quality), pronunciation dictionary, per-pipeline override |
| template | Template (house style: guide, palette, fonts, music mood); built-ins in code, workspace copies and agent drafts in the database |
| story | Story (idea, brief, sources, target keyword) owning deliverables: video / article / change as lead, derived or translation; Revision (thumbnail variants, team note); Feedback |
| publishing | Channel (YouTube, sealed refresh token), BlogTarget (becomes the site's git settings), Publication, pull request |
| site | Site (address, page limit, schedule), Crawl (facts: robots.txt, sitemaps, llms.txt, AI crawlers blocked), Page, Finding (rule + page; open, resolved, ignored) |
| analytics | Search Console connection, daily page and query metrics |
| visibility | Prompt, Run, Answer (mentions, citations) |
| job | Job (queue, kind, unique key, attempts, lease) |
| settings | Host profiles (throttle) and credentials (sealed, override env) |
| defaults | Workspace production defaults (model, effort, self-review, render quality and frame rate, first review, media sources, thumbnails) and default template |
| notify | Email digests |

A derived deliverable starts once its lead is live (published, or ready when no publisher
is connected), so an article can embed the published video. Approving an article queues
its translations.

### Settings layers

Every production setting resolves top down: built-in → **Server** (env, then the
settings page: credentials and the host profile, kept by the operator on `/system`) →
**Workspace** (Settings › Production, owners) → **Pipeline** or **ad-hoc request**
(editors). An unset value inherits from the layer above; the resolved values are copied
into the story's brief at planning, so later changes leave planned stories alone. The
UI shows each setting beside what it inherits and where from. The throttle is changed
only on `/system`; Jobs shows paused hosts and links there.

The template follows the same chain: a pipeline or request picks a template, picks none
(`noTemplate`), or takes the workspace default. The server's Claude model and effort are
resolved into the brief at planning too, so remote workers draft with the model shown in
the UI; they keep their own keys and Claude login from their own environment.

## 6. Production flow

1. Pick idea (source strategy) → dedupe (FTS5 trigram candidates + Jev verdict) → brief.
2. Seed a working directory: brief, referenced pages only, style tokens, tested animation
   components, the composition contract.
3. Claude Code writes the script → `tts` tool (word timings) → `composition.html` +
   `scenes.json` + `music.js`, checking its work with `preview_frame(t)`.
4. Validate (schema, duration vs audio, no network during render) → preview render (540p).
5. Review → feedback → `claude --resume` on the affected stages only → new revision.
   A long video's preview also draws its thumbnail variants (`thumbnail.html`); notes
   on the thumbnails alone, or reworded variants, redraw only them.
6. Approve → final render (1080p) → publish (with the picked thumbnail) → cross-link
   video ↔ article.

Long-form video is the same flow per chapter, concatenated with ffmpeg.

A short cut from a long video is the same flow, except that `prepare` writes the slice of
the long video's narration (and a ±15 s window of sentences around it) in place of `tts`,
and the agent only designs the picture; `ekin tool clip` moves the cut by whole sentences.

A change follows the same shape without speech or render: brief (the findings or the
opportunity, the affected pages' inventory rows) → shallow clone of the base branch in
the work dir → the agent edits, runs `ekin tool build` until it passes → `ekin tool diff`
records the revision's patch → review shows the diff → feedback resumes the session on
the same clone → approve → branch and pull request (or a commit) → merging → published.
The next crawl rechecks the findings it claimed to fix.

## 7. External prerequisites

- **YouTube Data API audit**: until it passes, API uploads are locked private and cannot
  be made public. Apply on day one. Move the OAuth consent screen to "In production"
  (testing-mode refresh tokens expire after 7 days). Check upload quota in Cloud Console.
- **Claude Code on the server**: `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN`.
  Subscription use for headless runs is a moving policy; keep the `Author` port swappable.
- **Search Console**: add the `webmasters.readonly` scope to the Google OAuth consent
  screen (a sensitive scope: Google verifies the app before other people's sites can
  connect).
- **GitHub token for changes**: contents and pull requests write access on the site's
  repository (fine-grained token), more than the contents-only token articles need.

## 8. Milestones

All built; ✅ verified end to end on the Mac with the real agent, TTS and renderer unless
noted.

0. Spikes — ✅ okur word timings (no retraining: letter durations → word frames, 40 ms
   frames, median error 20 ms); ✅ render benchmark (docs/benchmarks/render.md).
   Outside the repo: submit the YouTube API audit; try `claude -p` on the VPS.
1. Skeleton — ✅ layered module, SQLite + migrations, job queue, scheduler, pipelines, web shell.
2. Identity — ✅ users, sessions, workspace roles, invitations, CLI bootstrap.
3. Knowledge base — ✅ PDF / TXT / MD / EPUB upload, page extraction, token estimates,
   viewer with PDF pane, page-range refs with token totals.
4. Shorts MVP — ✅ prompt / knowledge / SEO pipelines, ad-hoc requests, Claude Code
   drafting with okur narration, Tone.js music, preview render, review with timestamped
   feedback resuming the agent session, approval, 1080p final.
5. Dedupe (trigram + Jev), notifications (feed + SMTP digests), host profiles editable at
   runtime, system checks, hybrid worker API — ✅ (SMTP and Jev against fakes only).
6. YouTube publishing — built and tested against a fake Google; needs OAuth credentials
   and the API audit to go live.
7. Articles — ✅ Turkish article, English translation, publish to a site checkout; GitHub
   commit tested against a fake API; video → article derivation with embed.
8. OpenSEO — ✅ opportunity pool, import, SEO pipeline source. Refresh calls OpenSEO's
   MCP endpoint directly with an API key (JSON-RPC over HTTP, `internal/adapter/openseo`)
   and ranks keywords in Go (`seo.Pick`); the agent plus Claude Code plugin remains the
   fallback without a key. Not run against the live service (uses credits).
9. Long-form — 16:9 compositions with scenes and YouTube chapters (enforced by the
   check), chunked rendering; ✅ a 2-minute video produced end to end (draft, review,
   1080p final) and cut into shorts; longer, chaptered videos not yet.

Site growth (see §1):

10. Pull requests — publish mode per blog target (commit or pull request), `merging`
    status, merge polling in `serve`, Awaiting merge in the UI; articles first. Built
    and tested against a fake GitHub; not yet run against a real repository.
11. Site — address, page limit and schedule (Settings › Site); crawl on setup, on demand
    and on schedule; page inventory; 23 audit rules raising findings that resolve,
    reopen or are ignored; Site page with findings by severity and rule, and the pages.
    ✅ crawled a local test site end to end through the UI. Core Web Vitals (PageSpeed
    Insights) not yet.

Planned:

12. Changes — `change` deliverable, repository work dir, `ekin tool build` and
    `ekin tool diff`, diff review, `findings` pipeline source.
13. Search Console — connection, daily pull, before/after on stories, digest lines.
14. AI visibility — prompt set, scheduled runs through OpenRouter, mentions and citations,
    `visibility` pipeline source.
15. Competitors — sitemap diffs as signals for SEO opportunities and change ideas.

## 9. Open decisions

- **License.** Ekin is FSL-1.1-ALv2 (source-available; each release turns Apache-2.0
  after two years). Calling it an open-source alternative to hosted growth platforms
  needs an OSI licence (it was AGPL-3.0 before); FSL keeps paid hosting with the author.
- **Name and positioning.** "Ekin" was named for a content studio; decide whether the
  site-growth product keeps it.
