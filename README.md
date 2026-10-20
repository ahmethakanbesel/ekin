# Ekin

Ekin is a self-hosted studio that turns ideas, your own documents and SEO keywords
into YouTube videos and blog articles. It writes, narrates and renders them unattended,
then holds every piece for a person to review before anything is published.

Videos and articles can be written in English or Turkish.

## What it does

Pipelines (cron or on demand) and ad-hoc requests turn a brief, knowledge-base pages or
SEO keywords into stories. For each story Claude Code (headless) writes the script and an
animated HTML composition, narrates it with Kokoro (English), okur-tts or EMA Lightning
(Turkish) or ElevenLabs (either), scores it with Tone.js, and Ekin renders it in headless
Chrome. Every deliverable waits in **Review**: request changes (pinned to a moment in the
video) and the agent revises in the same session; approve and it renders at 1080p and
publishes to YouTube, or commits the article (and its translation) to your site.

## Run with Docker

The `worker` image holds everything Ekin drives (Chromium, ffmpeg, poppler, uv for the
speech models and the Claude Code CLI), so one container can run the whole app:

```sh
git clone https://github.com/ahmethakanbesel/ekin.git && cd ekin
docker build --target worker -t ekin .

docker run -d --name ekin -p 8080:8080 -v ekin-data:/data \
  -e EKIN_HTTP_ADDR=0.0.0.0:8080 \
  -e EKIN_PUBLIC_URL=http://localhost:8080 \
  -e CLAUDE_CODE_OAUTH_TOKEN=<from `claude setup-token`> \
  ekin all

docker exec -it ekin ekin user create --email you@example.com --name You
```

Open http://localhost:8080 and sign in. **System** in the user menu shows which tools and
integrations are ready. Pass any other setting from `deploy/.env.example` with `-e` or
`--env-file`. Speech model weights download to the volume on first use.

### On a server

`deploy/compose.yaml` runs the API (a small distroless image) and the worker as separate
services, with Litestream replicating the database to S3-compatible storage and restoring
it onto an empty volume. It is tuned for a small shared VPS (the worker is pinned to one
core and capped at half of it); adjust the limits to your host.

```sh
cp deploy/.env.example deploy/.env   # set EKIN_PUBLIC_URL, the LITESTREAM_* bucket and your keys
docker compose -f deploy/compose.yaml up -d
docker compose -f deploy/compose.yaml exec serve /ekin user create --email you@example.com --name You
```

Put it behind a reverse proxy with HTTPS and set `EKIN_PUBLIC_URL` to that address.

## Run from source

Needs Go 1.26, Bun, ffmpeg, poppler (`pdftotext`), uv, Google Chrome and the Claude Code
CLI signed in (`claude`).

```sh
make build                                                  # web UI + bin/ekin
bin/ekin user create --email you@example.com --name You    # first account (admin); prompts for a password
make run          # API, web UI, scheduler and worker in one process on :8080
make test         # go test -race ./...
make lint         # golangci-lint, including the layer-boundary rules
make generate     # regenerate the Go server and TS client from api/openapi.yaml
```

Data lives in `./data` (`EKIN_DATA_DIR`). Configuration is `EKIN_*`
environment variables; see `internal/config/config.go` for every setting and
its default.

## Processes

| Command | Runs |
|---|---|
| `ekin serve` | API under `/api/v1`, embedded web UI, cron scheduler |
| `ekin worker` | Job runner, throttled by `EKIN_QUEUE_CONCURRENCY`, `EKIN_HEAVY_EXCLUSIVE`, `EKIN_HEAVY_QUEUES`, `EKIN_HEAVY_COOLDOWN`, `EKIN_CPU_LIMIT`, `EKIN_NICE` |
| `ekin all` | Both, for a single machine |
| `ekin migrate up\|down\|status` | Schema migrations (also applied at start unless `EKIN_MIGRATE_ON_START=false`) |
| `ekin user create\|reset-password` | Accounts; the password comes from `EKIN_PASSWORD`, a prompt or stdin |
| `ekin tool tts\|frame\|check\|stock\|music\|thumbnail` | The authoring agent's tools (run by the agent in its work dir) |

## Templates

A template is a house style: palette, fonts, motion, structure, narration voice and music
mood, written as a Markdown guide the agent follows (`STYLE.md` in its work dir). Pick
one per pipeline or ad-hoc request. Ten built-ins ship with Ekin, several inspired by the
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

## Captions

Every narrated video revision offers `captions.srt` and `captions.vtt` under **Files** in
the review panel and on the story page. They are made on request from the narration's word
timings (written words, not the pronunciation dictionary's respellings): sentences end a
cue, and a cue holds at most two lines of 42 characters and six seconds. Upload either to
YouTube Studio or a player as a caption track.

## Thumbnails

A long video gets designed thumbnails instead of a grabbed frame: the agent draws three
variants (Production → Thumbnail variants: 0–4) with different concepts, each with at most
five words. In review, the **Thumbnails** section shows them at full size and at a phone
feed's size beside the title. Pick the one the video goes out with (the agent's
recommended one otherwise), change a variant's words in place, or send notes about the
thumbnails alone. Neither renders the video again; the variants are redrawn in seconds.
Publishing uploads the picked variant; a channel YouTube has not verified for custom
thumbnails keeps YouTube's frame, and the "published" notification says so. Every variant
stays downloadable, one by one or as `thumbnails.zip`. Shorts keep a frame of the video.
See `docs/thumbnails.md`.

## Team notes

Every revision can carry one team note (**History** → *Add a team note*): what was
decided, what to remember. Editors write and change it at any status; viewers read it.
The agent never sees it. It is not feedback; use **Request changes** for that.

## Voice

**Settings → Voice** sets how narration sounds, per language: the engine, then its voice,
speed, sentence and paragraph pauses.

| Engine | Language | Runs | Options |
|---|---|---|---|
| Kokoro (default) | English | on the worker (CPU) | 28 American and British voices; model quality fp32 / fp16 / int8 |
| okur-tts (default) | Turkish | on the worker (CPU) | seed: each seed gives a different take of the same script |
| EMA Lightning | Turkish | on the worker (CPU), about 15× faster than real time | seed |
| ElevenLabs | English, Turkish | ElevenLabs API, billed per character | the account's voices, model, stability, similarity, style, speaker boost, seed |

ElevenLabs needs an API key (System → Credentials or `EKIN_ELEVENLABS_API_KEY`); without one
it cannot be saved or previewed. The narration text is sent to ElevenLabs, and every take is
billed: the agent may narrate more than once while fitting the script to the video. Each
paragraph is one request (long ones are split at sentence ends), with the neighbouring text
sent along so the intonation carries over. Word timings come from ElevenLabs' character
timestamps.

**Preview** renders a sample on a worker. The pronunciation dictionary maps a written word to
how it should be said ("Acme" → "ek mi", "CBT" → "si bi ti"), for every engine.
Captions keep the written word. Pipelines and ad-hoc requests can override the engine, voice
and speed.

## Integrations

Everything optional degrades gracefully; **System** in the user menu shows what is set up.
API keys and service settings (Claude token and model, Pexels, OpenRouter, SMTP, YouTube
OAuth) can be entered under **System → Credentials** (admins); they are stored encrypted
with the instance key (`data/secret.key` or `EKIN_SECRET_KEY`) and override the
environment variables below, which remain the fallback.

| Feature | Settings |
|---|---|
| Authoring agent | `claude` signed in (Mac) or `CLAUDE_CODE_OAUTH_TOKEN` (server); `EKIN_CLAUDE_MODEL`, `EKIN_CLAUDE_EFFORT` (`low` … `max`; the CLI's default when unset) |
| Speech | `tools/speech` scripts via `uv` (weights download on first use) |
| Stock footage | `EKIN_PEXELS_API_KEY` |
| Shorts from long videos | `EKIN_SHORTS_SOURCE_MIN/MAX` (4m / 60m), `EKIN_SHORTS_CLIP_MIN/MAX` (20s / 60s), `EKIN_SHORTS_MAX_PER_VIDEO` (5), `EKIN_SHORTS_MIN_GAP` (30s) — see `docs/shorts.md` |
| ElevenLabs narration | `EKIN_ELEVENLABS_API_KEY`, or System → Credentials. A hybrid worker reads only its own environment. |
| Email digests | `EKIN_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM` |
| Duplicate verdicts | `EKIN_OPENROUTER_API_KEY` (Jev) |
| YouTube | `EKIN_YOUTUBE_CLIENT_ID/SECRET`; redirect URI `<EKIN_PUBLIC_URL>/api/v1/oauth/youtube/callback`; then connect the channel in workspace settings |
| Blog | Workspace settings → GitHub repo + token, or a local checkout |
| OpenSEO | An API key (`oseo_…`, from OpenSEO) under System → Credentials or `EKIN_OPENSEO_API_KEY`: refreshes call OpenSEO's endpoint directly (`research_keywords` per seed, `get_domain_keyword_suggestions` per competitor) and Ekin ranks the results itself. Without a key, the worker host's Claude Code with the OpenSEO plugin connected does the refresh. |
| Hybrid worker | Server: `EKIN_WORKER_TOKEN`. Mac: `EKIN_REMOTE_URL=… EKIN_WORKER_TOKEN=… ekin worker` |

Throttling lives in **host profiles** (System page, admins; the Jobs page has quick
controls): per-queue concurrency, heavy exclusivity and which queues count as heavy,
cooldown, pause, CPU limit, job priority, render duty cycle, chunks and scales. Heavy
queues default to agent, tts and render; on a Mac, drop agent so drafts (which mostly
wait on Claude) run beside a render. Job priority (nice 0–19, Linux and macOS) lowers the
job processes' scheduling priority so the host's own work comes first. The CPU limit (Linux) keeps the job processes (Claude Code,
Chrome, ffmpeg, speech) under a percent of the host's CPUs by pausing them with SIGSTOP
whenever they run over, as cpulimit(1) does; the Ekin server itself is never paused. A worker uses the profile named by `EKIN_WORKER_PROFILE`. The server's
own worker applies a save at once, waking slots that are idle or cooling down; a hybrid
worker fetches its profile from the server every few seconds and keeps its own `EKIN_*`
values until a profile of its name is saved. Running jobs finish; a pause stops new ones.

## Accounts

There is no sign-up page. The first account comes from `ekin user create`; it
also becomes the owner of any workspace created before accounts existed.
Everyone else is invited from a workspace's Members page: the owner copies the
link (valid 7 days) and sends it. Roles are per workspace: **owner** (settings
and members), **editor** (pipelines, runs, review), **viewer** (read only).

Sessions are HttpOnly cookies (30 days idle, 90 days at most).
`EKIN_PUBLIC_URL` must be the address people open in the browser
(`https://...` in production): it prefixes invitation links, is the trusted
origin for writes, and an https URL marks the cookie Secure.
`ekin user reset-password` recovers a locked-out account and signs it out
everywhere.

## Contributing

See `CONTRIBUTING.md`. `PLAN.md` describes the product and its decisions;
`CONVENTIONS.md` the code.

## License

Ekin is free software under the [GNU Affero General Public License v3.0](LICENSE) or
any later version. If you run a modified Ekin as a service other people use, the AGPL
asks you to offer them its source.
