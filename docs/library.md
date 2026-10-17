# Media library — plan

A workspace keeps the music, stock videos and stock photos its videos use, so later
videos can reuse them. Items get into the library in two ways: Kurgu saves what the
agent composed or fetched from Pexels (each kind switched on separately, all off by
default), or someone uploads them. Each pipeline, and each ad-hoc request, picks per
kind where media comes from: freshly made or searched, the library only, or library
first.

## 1. Where we are

| Today | Effect |
|---|---|
| `kurgu tool stock` searches Pexels, transcodes clips to a ≤ 15 s VP8 WebM in `assets/` and appends to `assets/credits.json` | Every draft searches and downloads again; a good clip found once is gone after that revision. No Pexels key means no stock at all. |
| The agent writes `music.js` (Tone.js), `kurgu tool music` and the render step turn it into `music.wav` | A score that worked is never reused; there is no way to use a real track. |
| `Renderer.Mix` takes any audio file as music (`volume=0.28`, side-chain ducked under the voice) | A library track can go through the same mix unchanged; only looping, offset and fade are missing. |
| Work dirs are archived per revision | Items reused from the library are copied into the work dir, so past revisions stay reproducible even after a library item is deleted. |

## 2. Model — new `library` context

```
internal/domain/library/v1/      Asset, Usage, Collect, Policy
internal/repository/library/v1/  sqlite + inmemory + conformance suite
internal/service/library/v1/     upload, ingest, harvest, list, catalog, settings
internal/transport/http/library/v1/
internal/transport/job/library/v1/   library.ingest, library.harvest
```

**Asset** (aggregate):

| Field | Notes |
|---|---|
| id, workspace id | UUIDv7; every query takes the workspace |
| kind | `music` · `video` · `photo` |
| origin | `generated` (agent's Tone.js score) · `pexels` · `upload` |
| status | `processing` → `ready` · `failed` (uploads and harvests are ingested by a job) |
| title, description, tags | searchable; the agent reads them in the catalog |
| mood, tempo (music) | free text + BPM if known; matched against the template's `MusicMood` |
| orientation, width, height | `portrait` · `landscape` · `square` (video, photo) |
| duration | video / uploaded music; nil for a generated score (it fits any length) |
| files | blob keys: `main` (WebM / JPEG / audio / `music.js`), `poster`, `preview` (30 s Opus for music) |
| sha256 | of `main`; with the workspace it is unique, so the same file is stored once |
| source ref | `pexels:video:123` and page URL; unique per workspace with the origin, so a clip is harvested once |
| credit | author, URL, licence line; copied into `assets/credits.json` whenever the item is used |
| derived from | an earlier asset when the agent edited a reused score |
| created by, created at, archived at | archived items leave the catalog but keep their history |

Domain rules on it: `New…` per origin guards the fields that origin needs (a Pexels item
needs its source ref and author, an upload needs a credit or the "own work" flag);
`a.Ingested(files, meta, now)`, `a.Failed(reason)`, `a.Retag(...)`, `a.Archive(now)`,
`a.Fits(orientation)` (a portrait short only gets portrait or square media).

**Usage**: (asset, story, deliverable, revision, at). Written when a revision is
finished, so the library shows "used in" and the catalog can put rarely used items first.

**Collect** (per workspace): `music`, `video`, `photo` — booleans, **all false** by
default. Stored in a `library_settings` row; a missing row means all off.

**Policy** (per pipeline, copied into the story's `Brief` at planning like `Voice` and
`Template`; also on `AdHocRequest`):

| Mode | Music | Video / photo |
|---|---|---|
| `fresh` (default — today's behaviour) | the agent composes `music.js` | `kurgu tool stock` searches Pexels |
| `library` | only library tracks; `music.js` is refused | only library items; Pexels search is refused |
| `mixed` | library first, compose when nothing fits | library first, search Pexels when nothing fits |

A mode restricts where media comes from; it does not force the agent to use any. A
library-only pipeline with an empty library simply gets no stock or no music, and the
pipeline form warns ("no portrait videos in the library yet"). `library` mode needs no
Pexels key.

## 3. Getting items in

### Harvest (automatic, opt-in per kind)

1. When a video deliverable is **approved** and the workspace collects at least one kind,
   the approval enqueues `library.harvest` (io queue, unique key per revision) in the
   transaction that saves the decision. Approved is the useful signal: rejected drafts
   do not fill the library with rejected footage.
2. The job restores the revision's work dir (`ensureWorkDir`) and reads
   `assets/credits.json`. Only files that `composition.html` actually references are
   taken; downloaded-but-unused clips are skipped.
3. Per kind that is switched on:
   - **Pexels video / photo** — the transcoded WebM or JPEG as it is in `assets/`, plus a
     poster frame, with author, page URL, query (as a first tag) and Pexels' alt text
     (as the description). Skipped when its source ref already exists.
   - **Generated music** — `music.js` plus a 30 s preview rendered with `Renderer.Music`.
     Title and tags come from a `music.meta.json` the agent writes beside the score
     (mood, tempo, one-line description; asked for in the prompt). Skipped when the
     score's sha256 already exists. A score that started from a library item and was
     edited becomes a new asset with `derived from`.
4. Items from the library itself are not harvested again; their use is a Usage row.

`credits.json` entries gain `kind`, `externalId`, `width`, `height` and `alt` so the
harvest does not parse file names. Old entries without them are parsed from the
`pexels-<kind>-<id>` file name.

### Save from a revision (manual)

The review panel lists a revision's stock and music with **Save to library**, whatever
the collect switches say. Same code path as the harvest, for one item.

### Upload

`POST /library/assets` (multipart, one or more files, with kind, title, tags and credit)
stores the original under `ws/<ws>/library/<id>/original` and enqueues `library.ingest`:

| Kind | Accepted | Ingest |
|---|---|---|
| video | MP4, MOV, WebM; ≤ 500 MB, ≤ 120 s | probe; silent VP8 WebM at most 1920 px on the long side (the same `Transcode` as stock, longer cap); poster at 1 s |
| photo | JPEG, PNG, WebP; ≤ 25 MB | decode, strip metadata, JPEG at most 2160 px on the long side |
| music | MP3, WAV, FLAC, M4A, Ogg; ≤ 50 MB, ≤ 15 min | probe; `loudnorm` to the level generated scores reach, so the mix's fixed `volume=0.28` suits both; Opus `main` and 30 s preview |

The original is deleted after a successful ingest. A failed ingest keeps the item as
`failed` with the ffmpeg reason and a retry action, like knowledge documents.

## 4. Using items in a draft

`RunDraft` touches no database and may run on a remote worker, so the library reaches
the agent in two steps:

1. **Catalog (server, `prepare`).** For each kind whose mode is `library` or `mixed`,
   write `library/catalog.json`: ready, unarchived items that fit the frame's
   orientation, least used first, with id, title, description, tags, mood, duration,
   size and usage count — metadata only, no files. `LIBRARY.md` explains how to use it.
   Music matching the template's mood is listed first.
2. **Fetch (tool).** The agent runs:
   - `kurgu tool library --type video --query "sea at dusk"` — searches the local
     catalog (title, tags, description), prints the best matches. No network.
   - `kurgu tool library use <id>` — puts the file in place and records the credit
     (with `"library": "<id>"`) in `assets/credits.json`:
     - video / photo → `assets/library-<id>.webm|jpg`
     - generated music → `music.js` (the agent may adapt it)
     - uploaded music → `music.json` `{"asset": "<id>", "file": "assets/library-<id>.opus", "offset": 0}`

   Locally the tool opens the blob store from the inherited `KURGU_BLOB_*` config. On a
   remote worker it calls `GET /api/v1/worker/jobs/{id}/library/{assetId}`, scoped to
   the leased job's workspace and refused for an item not in that job's catalog. Both
   cache by sha256 under `KURGU_DATA_DIR/cache/library`.

**Enforcement** — in the tools and in Kurgu's own `validate`, not only in the prompt.
`agenttool.Spec` gains `Media` (the three modes):

- `kurgu tool stock --type video` fails in `library` video mode: "this pipeline uses
  library videos only: run kurgu tool library --type video".
- `kurgu tool music` and `check` fail when `music.js` exists in `library` music mode
  and does not come from a library score (its sha256 is in the catalog).
- `check` fails when a `credits.json` entry used by the composition has no `library`
  id in `library` mode, and when both `music.js` and `music.json` exist.

**Render.** `RunRender` uses `music.js` as today, or else `music.json`'s file: the
renderer gains `MusicTrack(file, offset, duration, out)` (trim from offset, loop when
shorter, 2 s fade-out) and the result goes through the existing `Mix`.

**Usage.** `FinishDraft` reads the library ids in `credits.json` and `music.json` and
writes Usage rows with the revision.

**Prompts.** `video.md` and `kit.md` get the media section per mode: `fresh` reads as
today; `library` names only `kurgu tool library`; `mixed` says to look in the library
first and search or compose only when nothing fits. When harvesting music is on, the
prompt asks for `music.meta.json`.

## 5. API

Spec operations (cursor paging and filters like stories and jobs):

| Operation | Access |
|---|---|
| `GET /workspaces/{ws}/library/assets?kind&origin&orientation&q&cursor` | view |
| `POST /workspaces/{ws}/library/assets` (multipart) | edit |
| `GET · PATCH · DELETE /workspaces/{ws}/library/assets/{id}` | view · edit · edit |
| `POST /workspaces/{ws}/library/assets/{id}/retry` | edit |
| `GET /workspaces/{ws}/library/assets/{id}/usages` | view |
| `POST /workspaces/{ws}/stories/{id}/deliverables/{did}/revisions/{n}/library` (save items from a revision) | edit |
| `GET · PUT /workspaces/{ws}/library/settings` (collect switches) | view · manage |

Pipelines and ad-hoc requests gain `media: {music, video, photo}` with `fresh` as each
default, so existing clients keep today's behaviour.

Outside the spec (CONVENTIONS §11 gains a third route group):
`GET /api/v1/workspaces/{ws}/library/assets/{id}/{file}` — `main`, `poster`, `preview`
with Range support, same session and `view` check as revision media. And the worker
route in §4.

Delete removes the blobs and the row; Usage rows keep the story link with the asset
shown as "deleted". Revisions are unaffected because they hold their own copies.

## 6. Web UI

- **Library** in the workspace nav (`/w/$ws/library`), after Knowledge. Tabs Music ·
  Videos · Photos; filters origin, orientation, search; a grid of posters (videos play
  muted on hover, music cards have a play button over the preview). Empty state explains
  both ways in: upload, or turn on saving in Settings.
- **Upload** — drop zone on the page and an Upload button; several files at once, each
  row with title, tags and credit (or "own work"), progress, then the processing state.
- **Item sheet** — preview, edit title / tags / description / mood, credit, "Used in"
  with links to stories, archive, delete.
- **Workspace settings** → *Library*: "Save to the library when a video is approved"
  with three switches (generated music, Pexels videos, Pexels photos), all off.
- **Pipeline form and ad-hoc form** → *Media*: one select per kind (Generate / search ·
  Library only · Library first) with the number of fitting library items beside it.
- **Review panel** → the revision's media list with **Save to library** and a
  "from library" badge on reused items.

## 7. Phases

Each phase ships on its own with tests (domain table tests, repository conformance,
service with inmemory repos and a fixed clock, `httptest` through the router, the
agent tool against a fake blob store and a fake worker API).

1. **Library context and uploads** — Asset aggregate, migration (`library_assets`,
   `library_usages`, `library_settings`), upload + `library.ingest`, list / get / edit /
   delete / media route, Library page with upload. Useful alone as a place to keep media.
2. **Using the library** — Policy on pipelines, ad-hoc requests and `Brief`; catalog in
   `prepare`; `kurgu tool library` (local blob store); `music.json` + `MusicTrack`;
   Usage rows; prompts; enforcement in tools and `validate`; Media section in the
   pipeline and ad-hoc forms.
3. **Harvest** — collect settings and UI, richer `credits.json`, `music.meta.json`,
   `library.harvest` on approval, Save to library in the review panel.
4. **Remote workers** — worker API route for library files and the tool's remote fetch
   with the sha256 cache. Folds into phase 6 (server-side tool proxy) of
   `docs/remote-workers.md` if that lands first.

When a phase lands, add the library to PLAN.md §1 (Scope), §5 (Bounded contexts: a
`library` row with Asset, Usage) and the *Stock media* / *Music* rows of §2.

## 8. Open questions

- Harvest on approval (assumed) or already when a draft finishes? Approval keeps the
  library clean; on draft fills it faster.
- Stored Pexels clips are the deliverable's 15 s rendition in its orientation. Should a
  harvest download a longer, larger rendition once (one more Pexels request) so clips
  also serve other orientations and lengths?
- Should a pipeline avoid repeating the same item within N days or N stories, as a hard
  rule in the catalog, or is "least used first" enough?
- Pexels' licence allows reuse in our own videos but not redistributing the files as a
  stock collection; the library stays private to the workspace and is never exported in
  bulk. Is that acceptable as the only safeguard?
