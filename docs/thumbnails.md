# Thumbnail variants — plan

A video gets a few designed thumbnails instead of one grabbed frame. The agent draws
two to four variants with different concepts on a page of their own, the render step
turns them into full-size JPEGs, and the reviewer picks one before approving it (or
sends notes on the thumbnails alone, without a new video render). Publishing uploads the
picked variant. Once the video is live, every variant stays available to download.
Long videos only: shorts keep today's poster frame.

## 1. Where we are

| Today | Effect |
|---|---|
| `RunRender` grabs `poster.jpg` from the rendered MP4 at `min(1.5 s, duration/3)` (`review.go`) | The thumbnail is whatever is on screen in the first seconds: often half-built, no title text, at preview scale until the final render. |
| `publishVideo` uploads `poster.jpg` with `thumbnails.set` and logs the error when it fails | The upload path already exists. Only the file it sends changes. |
| `Renderer.Snapshot` renders a page at given times to JPEGs (`ekin tool frame`) | A static thumbnail page can be rendered with the same Chromium setup. No new render engine is needed. |
| Feedback scopes are `script · voice · visuals · music · metadata` | There is no scope for the thumbnail. Every note leads to a new video render. |
| `edit.go` carries `preview.mp4` and `poster.jpg` over when an edit leaves the picture unchanged | A thumbnail-only revision can reuse the same carry-over, so it costs no video render. |
| The inbox poster and `<video poster>` use `poster.jpg` at 9:16 or the real shape | These stay as they are. Thumbnails are a separate artifact. |

## 2. What a variant is

A **variant** is one concept, not a colour tweak of another variant. The prompt asks
for distinct approaches, for example:

- **frame** — a strong still from the video (rendered at full scale) with a 2–4 word hook;
- **type** — bold typography, a number or a short claim on the template's palette;
- **contrast** — a before/after, a question, or two things set against each other.

Each variant has a number (1…N), a concept label, its **text** (the words on the
thumbnail, in the video's language) and, optionally, the composition time of the still
it uses. The agent marks one variant as **recommended**. When the reviewer picks
nothing, the recommended variant is used.

The text is never the video's title. It sits next to the title in YouTube's grid, so
it adds to the title instead of repeating it.

## 3. Contract — the agent's side

New files in the work dir:

- `thumbnails.json` — `{"recommended": 2, "variants": [{"n": 1, "concept": "frame",
  "text": "...", "still": 12.4}, ...]}`.
- `thumbnail.html` — one static page for all variants. It loads the kit and reads
  `thumbnails.json`, and `window.ekin.thumbnail(n)` lays out variant n. The words come
  from the JSON and are never typed into the page, so a person can edit them later
  without the agent (§6).
- `thumbnails/stills/<t>.jpg` — full-scale composition frames that
  `ekin tool thumbnail --stills 12.4,30` renders for the page to use as `<img>`.

`ekin tool thumbnail [--n 2]` renders `thumbnails/<n>.jpg` at full size and
`thumbnails/<n>.small.jpg` at 320×180. The small one is how the thumbnail looks in a
phone feed, and the agent is told to judge it at that size. The tool checks the
following. Its errors block the draft, as `check` does:

- 2–4 variants (the pipeline's count, §5), unique numbers, recommended among them;
- the page loads without errors and references nothing on the network (reuse
  `externalRef`);
- each variant's rendered text contains its `text` from the JSON, which is at most
  5 words;
- no text element overlaps the bottom-right corner (YouTube's duration badge, about
  the last 18% × 14%);
- the JPEG is under 2 MB at the 1280×720 YouTube limit. The tool lowers the quality
  until it fits.

`video.md` gains a step after metadata: write the thumbnails, render them, look at the
small versions, fix them. `revise-thumbnail.md` is the prompt for thumbnail-only
feedback: change only `thumbnail.html` and `thumbnails.json`, and leave everything
else untouched.

## 4. Model — in the `story` context

**Thumbnail** (value): `n`, `concept`, `text`, `still` (optional seconds). It is kept on
the **revision** (`rev.Thumbnails`, plus `Recommended`) so the review screen can list
variants without reading a blob. The files are revision artifacts named
`thumb-<n>.jpg`. The names are flat so the media route's single `{artifact}` segment
and the worker upload allow-list still work.

**Deliverable** gains `thumbnail` — the picked variant's number, 0 when none is picked.

Domain rules:

- `ParseThumbnails(raw, want)` — parses `thumbnails.json` and checks counts, numbers,
  word limit and the recommended variant. Used by `validate` and by the finish step.
- `st.PickThumbnail(did, n)` — allowed while the video is in review or ready, not
  once it is published. n must exist on the current revision. A new revision resets
  the pick to 0.
- `d.ThumbnailFor(rev)` — the picked number, else the recommended one, else 0 (fall
  back to `poster.jpg`).
- `ScopeThumbnail` joins the feedback scopes. Feedback whose notes are all
  `thumbnail` is a **thumbnail-only revision** (`fb.ThumbnailOnly()`): the agent
  resumes with `revise-thumbnail.md`, and the render job runs in thumbnail mode (§5).

**Persistence**: `deliverables.thumbnail INTEGER NOT NULL DEFAULT 0`;
`revisions.thumbnails TEXT` (JSON of variants and recommended). The repository
conformance suite covers both columns.

## 5. Flow

1. **Setting.** `Production.Thumbnails` (pipeline and ad-hoc request): 0–4, default 3.
   0 keeps today's grabbed frame and skips the step in the prompt. Only long videos
   use it: `CheckFor` refuses a non-zero count without a long video output, and a
   short deliverable (automatic or cut from a long video) always runs with 0.
2. **Draft.** The agent writes and checks the thumbnails as in §3. `validate` runs
   `ParseThumbnails` and requires the page.
3. **Render.** `RunRender` (preview only) renders every variant with `Snapshot` at
   scale 1 into `thumb-<n>.jpg`, after `poster.jpg`. Thumbnails do not depend on
   preview quality, so the final render keeps the preview's thumbnails instead of
   rendering them again. `FinishRender` stores them and `rev.Thumbnails`.
   `RenderTask` gains `ThumbnailsOnly`: no video, music or mix, only the stills and the
   variants. The preview and poster artifacts carry over as `edit.go` does.
4. **Review.** The reviewer picks a variant (or keeps the recommended one), approves, or
   sends `thumbnail` notes. Those give a new revision in seconds, not a re-render.
5. **Publish.** `publishVideo` opens `thumb-<d.ThumbnailFor(rev)>.jpg`, falling back to
   `poster.jpg`. A failed `thumbnails.set` is still logged and does not fail the
   publication (an unverified channel cannot set custom thumbnails). It also
   raises a notification, so the person knows the channel needs verifying.

## 6. After the first draft

- **Edit the text in place.** Changing a variant's words in the review panel writes an
  edited revision (`st.Edited`, as for scripts) with the new `thumbnails.json` and
  queues a thumbnail-only render. No agent run is needed.
- **Download after publishing.** A published video's variants are download only:
  *Download variants* (a zip of the `thumb-*.jpg` files) and each one on its own.
  A person uploads another one in Studio by hand, for example to start Studio's
  Test & Compare. Ekin does not change a published video's thumbnail.

## 7. API

| Operation | Access |
|---|---|
| Revisions gain `thumbnails {recommended, variants[]}`, deliverables gain `thumbnail` | view |
| `PUT /workspaces/{ws}/stories/{id}/deliverables/{did}/thumbnail` `{variant}` | edit |
| `PATCH /workspaces/{ws}/stories/{id}/deliverables/{did}/thumbnails/{n}` `{text}` (edited revision) | edit |
| `thumb-<n>.jpg` through the existing revision media route; `thumbnails.zip` built on request | view |
| Feedback notes accept scope `thumbnail` | edit |
| Pipelines and ad-hoc requests gain `production.thumbnails` | edit |

Each new operation goes into the access table (`TestAccessRulesCoverSpec`).

## 8. Web UI

- **Review panel → Thumbnails.** The variants side by side at their real 16:9 shape
  (Real Shape Rule), each also at feed size (about 168×94) next to the metadata title
  and channel name, the way YouTube's grid shows it. Concept label, a *Recommended*
  badge, a radio to pick, inline text edit, and a note field that defaults to the
  `thumbnail` scope.
- **Story page** for a published video: the uploaded thumbnail, the other variants
  and *Download variants*. There is no picker here.
- **Pipeline and request forms**: "Thumbnail variants" (0–4) in the production
  settings, shown only with a long video output.
- The inbox poster stays `poster.jpg`. Its card is 9:16 for every draft, and a 16:9
  thumbnail would be cropped or letterboxed.

## 9. Phases

Each phase ships on its own with tests: domain table tests, repository conformance,
service tests with in-memory repos and a fixed clock, and `httptest` through the router.

1. **Generate.** `Production.Thumbnails`, `ParseThumbnails`, the contract, the kit helper,
   `ekin tool thumbnail`, the prompt step, render and store the variants, migration.
   Publishing uploads the recommended variant. From here every video gets a designed
   thumbnail.
2. **Pick and revise.** `PickThumbnail`, `ScopeThumbnail`, `revise-thumbnail.md`,
   thumbnail-only renders, the review panel.
3. **Edit and download.** In-place text edits, the variants on the story page, the
   variants zip.
4. **House style.** An optional thumbnail guide on templates (brand mark, corner, font
   weight) passed to the agent with `STYLE.md`. After that, the picked thumbnail as
   the article's cover or OG image, once the marketing site's schema has a field for it.

Built: phases 1–3. Where the build differs from the plan above:

- **Count.** `Production.Thumbnails` unset means 3; 0 turns thumbnails off; 1–4 asks for
  exactly that many variants. Outputs without a long video ignore the setting instead
  of refusing it, so switching a pipeline to shorts never fails to save.
- **Picking** is allowed while the video is in review, finalizing or ready.
  `EditedThumbnails` (not `st.Edited`) records a text edit: the deliverable goes to
  rendering, keeps its pick, and the revision carries the video and the old images
  until the variants are drawn again.
- **Thumbnail-only renders** are chosen by `Revision.ThumbnailsOnly()`: the revision
  answers thumbnail-only feedback or is a person's text edit, and the revision before
  it has a preview. The render draws the variants alone and carries the preview,
  poster, preview settings and length over.
- **A failed preview thumbnail** does not fail the preview render: the video goes to
  review with its poster frame and the failure is logged.
- **A refused `thumbnails.set`** is added to the "published" notification rather than
  sent as a notification of its own.
- **Downloads** are on the review panel's Thumbnails section in every status (each
  variant and `thumbnails.zip`); there is no separate story-page view.

## 10. Decisions

- **A page, not a frame.** Thumbnails need large text, a different crop and a different
  hierarchy than any moment of the video. A separate static page lets the agent
  design them while still reusing the video's stills, kit and palette.
- **Text in JSON.** Words a person may want to change live in `thumbnails.json`, so a
  text fix needs no agent run.
- **Render once, at full size.** Stills are cheap and scale-independent, so they are
  rendered with the preview and kept for the final render.
- **No shorts.** Shorts keep today's poster frame. The Shorts feed shows a frame
  chosen in the app, so designed variants would mostly go unseen.
- **Download only after publishing.** Ekin uploads the picked variant once, at
  publishing. Later changes and Studio's Test & Compare are done by hand with the
  downloaded files. There is no `publishing.thumbnail` job and no change history.
- **No CTR tracking.** Ekin does not read click-through rates or rank variants by
  performance.
