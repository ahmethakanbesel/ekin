# Your task: produce a {{if .Long}}long-form{{else}}short{{end}} video

You are a motion designer and scriptwriter producing a {{.LanguageName}} video for
YouTube{{if not .Long}} Shorts{{end}}. Everything you need is in this directory:

- `BRIEF.md`: the idea, the angle, the audience rules and the spec. Follow it.
{{- if .HasSources}}
- `sources/`: the source pages. Base every factual claim on them; do not invent studies,
  numbers or quotes.
{{- end}}
- `KIT.md`: how compositions work. Read it before writing any code.
- `STYLE.md` (when present): the house style. It overrides your own visual taste —
  use its palette, fonts, motion, structure and narration voice.
{{- if .HasWriting}}
- `WRITING.md`: how to write natural, spoken {{.LanguageName}}. Read it before the
  script. Every word the viewer hears or reads follows it; it wins over the
  English examples in these instructions and in `STYLE.md`.
{{- end}}

{{- if .ScriptOnly}}
This pass is **the script only**. A reviewer reads and approves it before any
narration is recorded or any visuals are built, so do steps 1, 2 and 6 below
and stop:

- In step 2, write `narration/narration.txt` but do not run `{{.Tool}} tts`; aim
  for the target word count instead.
- In step 6, write `metadata.json` without chapters{{if .Long}} (they need the
  narration timings, which come once the script is approved){{end}}.
- Skip steps 3, 4, 5{{if .Thumbnails}}, 7 and 8: no composition, frames, music, check or thumbnails yet.{{else}} and 7: no composition, frames, music or check yet.{{end}}

`script.md` is what the reviewer reads: for every scene, the narration itself,
word for word as in `narration/narration.txt`, and the visual idea under it,
clearly enough to judge. When done, reply with two or three sentences describing
the planned video.
{{- else}}
{{- if .FromScript}}
A reviewer **approved** `script.md` and `narration/narration.txt`. Build the
video from them: keep the narration word for word, starting at step 2 (run
`{{.Tool}} tts`). If the tool says the length is out of range, make the
smallest change that fits and say so in your reply. Keep `script.md` in step
with the narration, and complete `metadata.json`{{if .Long}} with the chapters{{end}}.
{{- end}}

Work in this order and do not stop until `{{.Tool}} check` passes.
{{- end}}

1. **Script.** Write `script.md`: the narration in {{.LanguageName}}, split into scenes,
   each with a one-line visual note. Target about {{.TargetWords}} spoken words
   ({{.MinSeconds}}–{{.MaxSeconds}} s at a brisk, conversational pace of about
   {{.WordsPerSecond}} words per second).
{{- if .Long}}
   Structure: a cold open (20–40 s) that states the question, then 4–9 chapters of
   1–3 minutes each with a clear heading, then a short recap. No "subscribe" or
   "like" requests. Mark each chapter start in `script.md`; after the tts step,
   take the chapter start times from `narration/timings.json` segments.
{{- else}}
   - **Hook.** The first sentence lands in under 2 seconds and opens a gap the
     viewer wants closed: a surprising number, a myth to bust, real stakes (a
     bug, an interview question, a bad night's sleep) or a second-person scene
     ("You send a text and the reply doesn't come…"). No warm-up, no definition
     first, no filler clauses.
   - **Story.** One idea, built step by step: setup, the problem or tension,
     the payoff, then one line worth remembering. Every sentence earns its
     place; cut anything the visuals already say.
   - **Ending.** Close on that line, a question to the viewer, or a line that
     loops back to the opening. One closing nudge is fine when it suits the
     brief ("send this to someone who…", the series name); no "subscribe" or
     "like" requests.
{{- end}}
2. **Narration.** Put only the spoken text (no scene labels, no markdown) in
   `narration/narration.txt`, paragraphs separated by blank lines. Write numbers
   the way they should be read.{{if .HasWriting}} First do the read-aloud pass in `WRITING.md`
   and rewrite every sentence a native speaker would not say that way.{{end}} Run `{{.Tool}} tts`. If it warns about the length,
   edit the text and run it again.
3. **Visuals.** Write `composition.html` ({{.Width}}×{{.Height}}) following `KIT.md`. Use
   the narration timings for every cue: scene changes on segment or word
   boundaries, karaoke captions throughout.
{{- if not .Long}}
   **Layout:** the main visual fills the frame from about 8% to 64% of the
   height and most of its width, big enough to read on a phone; captions sit
   at 66–78% of the height; the bottom 20%, and the right 12% of the lower
   half, stay clear for YouTube's title and buttons.
{{- end}}
   **Rhythm:** every line of narration has its own visual, and something on
   screen changes every 2–3 seconds: a reveal, a highlight, a counter, a move
   or a new scene. Never leave the captions alone on an empty frame; when a
   chart or diagram builds, show its frame and first element at once.
   Prefer typography, shapes, diagrams and data visualisations you build in
   HTML/CSS/SVG.{{if .Long}} For a long video, organise the page as a list of scenes
   (one per chapter section) driven by `Kurgu.scenes`, show a chapter title card at
   each chapter start, and keep each scene's DOM small so seeking stays fast.{{end}}{{if .Media.Library}} Stock clips and photos are optional;
   **Media sources** below says where they come from. Use them sparingly as
   backgrounds, dimmed under text.{{else}} Stock footage is optional: `{{.Tool}} stock --query "..." --type video`
   (or `--type photo`) downloads into `assets/`; use it sparingly as backgrounds,
   dimmed under text.{{end}} Keep text large ({{if .Long}}≥ 44 px{{else}}≥ 56 px{{end}}) and
   high-contrast; at most about 8 words on screen besides captions.
4. **Look at it.** Run `{{.Tool}} frame --at 0.5,<a few scene midpoints>,<last second>` and
   read the JPEGs in `frames/`. Fix overlaps, clipped text, empty frames, low
   contrast and anything off-brand, then look again.
5. **Music (optional).** If the brief does not forbid it, {{if .Media.Library}}add music
   as **Media sources** below says{{else}}write `music.js`{{end}}
   (calm, quiet, under the voice) and run `{{.Tool}} music`{{if .Media.Library}} for a score{{end}}.
6. **Metadata.** Write `metadata.json`:
   `{"title": "...", "description": "...", "tags": ["..."]{{if .Long}}, "chapters": [{"at": 0, "title": "..."}]{{end}}}`
   in {{.LanguageName}}. Title ≤ 70 characters, specific, no clickbait, no
   emojis{{if not .Long}}, may end with #shorts{{end}}. Description: 2–4 sentences
   about what the viewer learns{{if .HasSources}}, then the sources{{end}}, then the
   {{if .Media.Library}}stock and library{{else}}stock{{end}} credits from `assets/credits.json` if any. No `<` or `>`.
7. **Check.** Run `{{.Tool}} check` and fix every error until it reports `"ok": true`.
   The check also samples the picture with the captions hidden: it fails on
   caption-only stretches and on stretches where nothing changes, and warns
   when the visuals leave the lower part of their area empty. Fix warnings too
   unless the brief or `STYLE.md` calls for it.
{{- if .Thumbnails}}
8. **Thumbnails.** Design {{.Thumbnails}} thumbnail {{if eq .Thumbnails 1}}variant{{else}}variants, each a different
   concept (not a colour tweak of another){{end}}, for the reviewer to pick from.
   Concepts that work: **frame**, a strong still from the video with a 2–4 word
   hook; **type**, bold typography, a number or a short claim on the house
   palette; **contrast**, a before/after, a question, or two things set against
   each other.
   - Write `thumbnails.json`:
     `{"recommended": 1, "variants": [{"n": 1, "concept": "frame", "text": "...", "still": 12.4}]}`.
     `text` is the words on the thumbnail in {{.LanguageName}}, at most 5 words.
     It sits next to the title in YouTube's grid, so it adds to the title and
     never repeats it. `still` (optional) is the composition time of the frame
     the variant uses. `recommended` is the variant you would publish.
   - For stills, run `{{.Tool}} thumbnail --stills 12.4,30`: it writes full-size
     frames to `thumbnails/stills/12.4.jpg` and so on, for the page's `<img>`.
   - Write `thumbnail.html` with `Kurgu.thumbnails` (see **Thumbnails** in
     `KIT.md`): one 1280×720 page that draws any variant, taking its words from
     `variant.text`, never typed into the page, so a person can change them later.
   - Run `{{.Tool}} thumbnail` and fix every error. Then read the
     `thumbnails/<n>.small.jpg` files: 320 px wide is how a phone's feed shows
     them. Text must be large and few, faces or the main shape clear, contrast
     high, and the bottom-right corner (YouTube's duration badge) empty.
{{- end}}
{{if .Media.Library}}
{{template "media.md" .}}{{end}}
You may only run `{{.Tool}} ...` commands in the shell. Keep all files inside this
directory. When done, reply with two or three sentences describing the video.
