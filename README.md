# Kurgu

Unattended video and article production with human review. See `PLAN.md` for
the product and `CONVENTIONS.md` before writing code.

## What it does

Pipelines (cron or on demand) and ad-hoc requests turn a brief, knowledge-base pages or
SEO keywords into stories. For each story Claude Code (headless) writes the script and an
animated HTML composition, narrates it with okur-tts (Turkish) or Kokoro (English),
scores it with Tone.js, and Kurgu renders it in headless Chrome. Every deliverable waits
in **Review**: request changes (pinned to a moment in the video) and the agent revises in
the same session; approve and it renders at 1080p and publishes to YouTube, or commits
the article (and its translation) to the marketing site.

## Run on a laptop

Needs Go 1.26, Bun, ffmpeg, poppler (`pdftotext`), uv, Google Chrome and the Claude Code
CLI signed in (`claude`).

```sh
kurgu user create --email you@example.com --name You   # first account (admin); prompts for a password
make run          # API, web UI, scheduler and worker in one process on :8080
make test         # go test -race ./...
make lint         # golangci-lint, including the layer-boundary rules
make generate     # regenerate the Go server and TS client from api/openapi.yaml
```

Data lives in `./data` (`KURGU_DATA_DIR`). Configuration is `KURGU_*`
environment variables; see `internal/config/config.go` for every setting and
its default.

## Processes

| Command | Runs |
|---|---|
| `kurgu serve` | API under `/api/v1`, embedded web UI, cron scheduler |
| `kurgu worker` | Job runner, throttled by `KURGU_QUEUE_CONCURRENCY`, `KURGU_HEAVY_EXCLUSIVE`, `KURGU_HEAVY_COOLDOWN` |
| `kurgu all` | Both, for a single machine |
| `kurgu migrate up\|down\|status` | Schema migrations (also applied at start unless `KURGU_MIGRATE_ON_START=false`) |
| `kurgu user create\|reset-password` | Accounts; the password comes from `KURGU_PASSWORD`, a prompt or stdin |
| `kurgu tool tts\|frame\|check\|stock\|music` | The authoring agent's tools (run by the agent in its work dir) |

## Templates

A template is a house style: palette, fonts, motion, structure, narration voice and music
mood, written as a Markdown guide the agent follows (`STYLE.md` in its work dir). Pick
one per pipeline or ad-hoc request. Ten built-ins ship with Kurgu, several inspired by the
general look of well-known explainer channels (flat cosmic explainers, newsroom
explainers, animated lessons, precise diagrams, minimal icons, reflective essays, friendly
psychology, map journalism) plus "Clinical calm" and an article guide. **Draft from a
channel** has the agent research any channel on the web and write a new template. Every
template tells the agent to describe a style, never copy it: no channel names, logos,
mascots, characters, catchphrases or music.

## Editing in place

A deliverable in review can be edited directly instead of asking the agent: an article's
text and frontmatter, or a video's narration, title, description and tags. Each save is a
new revision. Text and metadata edits stay in review (a video keeps its preview). An edited
narration goes back to the agent, which keeps the text word for word, narrates it again
and re-times the visuals to it. That takes a minute or two.

## Voice

**Settings → Voice** sets how narration sounds, per language. Turkish uses okur-tts: speed,
sentence and paragraph pauses, and a seed. Each seed gives a different take of the same
script. English uses Kokoro with 28 American and British voices, speed, pauses and model
quality (fp32 / fp16 / int8). **Preview** renders a sample on a worker. The pronunciation
dictionary maps a written word to how it should be said ("acme" → "ek mi",
"CBT" → "si bi ti"). Captions keep the written word. Pipelines and ad-hoc requests can
override the voice and speed.

## Integrations

Everything optional degrades gracefully; **System** in the user menu shows what is set up.
API keys and service settings (Claude token and model, Pexels, OpenRouter, SMTP, YouTube
OAuth) can be entered under **System → Credentials** (admins); they are stored encrypted
with the instance key (`data/secret.key` or `KURGU_SECRET_KEY`) and override the
environment variables below, which remain the fallback.

| Feature | Settings |
|---|---|
| Authoring agent | `claude` signed in (Mac) or `CLAUDE_CODE_OAUTH_TOKEN` (server); `KURGU_CLAUDE_MODEL` |
| Speech | `tools/speech` scripts via `uv` (weights download on first use) |
| Stock footage | `KURGU_PEXELS_API_KEY` |
| Email digests | `KURGU_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM` |
| Duplicate verdicts | `KURGU_OPENROUTER_API_KEY` (Jev) |
| YouTube | `KURGU_YOUTUBE_CLIENT_ID/SECRET`; redirect URI `<KURGU_PUBLIC_URL>/api/v1/oauth/youtube/callback`; then connect the channel in workspace settings |
| Blog | Workspace settings → GitHub repo + token, or a local checkout |
| OpenSEO | An API key (`oseo_…`, from OpenSEO) under System → Credentials or `KURGU_OPENSEO_API_KEY`: refreshes call OpenSEO's endpoint directly (`research_keywords` per seed, `get_domain_keyword_suggestions` per competitor) and Kurgu ranks the results itself. Without a key, the worker host's Claude Code with the OpenSEO plugin connected does the refresh. |
| Hybrid worker | Server: `KURGU_WORKER_TOKEN`. Mac: `KURGU_REMOTE_URL=… KURGU_WORKER_TOKEN=… kurgu worker` |

Throttling lives in **host profiles** (System page, admins): per-queue concurrency, heavy
exclusivity, cooldown, pause, render duty cycle, chunks and scales. A worker uses the
profile named by `KURGU_WORKER_PROFILE` and picks up edits within 10 seconds.

## Accounts

There is no sign-up page. The first account comes from `kurgu user create`; it
also becomes the owner of any workspace created before accounts existed.
Everyone else is invited from a workspace's Members page: the owner copies the
link (valid 7 days) and sends it. Roles are per workspace: **owner** (settings
and members), **editor** (pipelines, runs, review), **viewer** (read only).

Sessions are HttpOnly cookies (30 days idle, 90 days at most).
`KURGU_PUBLIC_URL` must be the address people open in the browser
(`https://...` in production): it prefixes invitation links, is the trusted
origin for writes, and an https URL marks the cookie Secure.
`kurgu user reset-password` recovers a locked-out account and signs it out
everywhere.
