# Your task: cut a short from a long video

You are a motion designer turning one moment of an approved long video into a
{{.LanguageName}} YouTube Short. The moment is {{.ClipStart}}–{{.ClipEnd}} of the long
video ({{.ClipSeconds}} s). Its narration is already recorded: you design the
picture around it. Everything you need is in this directory:

- `BRIEF.md`: the idea, the audience rules and the spec. Follow it.
- `sources/clip.md`: the long video's sentences around this moment, numbered, with
  those this short speaks marked ▶.
- `sources/lead.md`: the long video's whole script and metadata, for context.
  The short must stand on its own for someone who never saw the long video.
- `sources/long/composition.html` (and `scenes.json` when present): how the long
  video looked. Reuse its palette, type and motion so the two feel like one
  series, but lay everything out anew for a vertical frame; do not copy its
  16:9 layout.
- `narration/speech.wav`, `narration/timings.json`, `narration/narration.txt`: the
  narration of this short and its word timings. **Do not change them and do not
  run `{{.Tool}} tts`**: the short speaks the long video's approved narration,
  word for word. To start or end at another sentence (a reviewer may ask for
  it), run `{{.Tool}} clip --first N --last M` with sentence numbers from
  `sources/clip.md`: it rewrites `narration/` for you; then re-time the
  composition to the new timings.
- `assets/` (with `assets/credits.json`): the stock media the long video used.
  Use what fits; you may add more.
- `KIT.md`: how compositions work. Read it before writing any code.
- `STYLE.md` (when present): the house style. It overrides your own visual taste.
{{- if .HasWriting}}
- `WRITING.md`: how to write natural {{.LanguageName}}, for every word on screen.
{{- end}}

Work in this order and do not stop until `{{.Tool}} check` passes.

1. **Plan.** Write `script.md`: the narration from `sources/clip.md`, split into
   scenes, each with a one-line visual note. Decide the hook: the first two
   seconds must make a viewer who never saw the long video stay. A title card
   with the short's question or claim, on screen from the first frame, does this
   when the first sentence alone does not.
2. **Visuals.** Write `composition.html` ({{.Width}}×{{.Height}}) following `KIT.md`.
   Load the timings with `Kurgu.timings()` and cue everything on them: scene
   changes on sentence or word boundaries, karaoke captions throughout.
   **Layout:** the main visual fills the frame from about 8% to 64% of the
   height and most of its width, big enough to read on a phone; captions sit
   at 66–78% of the height; the bottom 20%, and the right 12% of the lower
   half, stay clear for YouTube's title and buttons.
   **Rhythm:** every line of narration has its own visual, and something on
   screen changes every 2–3 seconds. Never leave the captions alone on an empty
   frame. Keep text large (≥ 56 px) and high-contrast; at most about 8 words on
   screen besides captions.
   **End.** The composition lasts the narration plus at most {{.TailSeconds}} s. Use
   the tail, if at all, for a short end card that points to the full video
   ("the full video is on the channel"); no "subscribe" or "like" requests.
3. **Look at it.** Run `{{.Tool}} frame --at 0.5,<a few scene midpoints>,<last second>` and
   read the JPEGs in `frames/`. Fix overlaps, clipped text, empty frames, low
   contrast and anything off-brand, then look again.
4. **Music (optional).** `music.js` (or `music.json`) is the long video's music,
   when it had any. Keep it, adapt it to the short's mood and length, or remove
   it; if you change `music.js`, run `{{.Tool}} music`.{{if .Media.Library}} To replace it, or to add
   stock, follow **Media sources** below.{{end}}
5. **Metadata.** Write `metadata.json`:
   `{"title": "...", "description": "...", "tags": ["..."]}` in {{.LanguageName}}.
   Title ≤ 70 characters, specific to this moment, no clickbait, no emojis, may
   end with #shorts. Description: 1–3 sentences about the moment,{{if .VideoURL}}
   then "Full video: {{.VideoURL}}",{{else}} then a line saying the full video is on the channel,{{end}}
   then the stock credits from `assets/credits.json` for the media you used. No
   `<` or `>`.
6. **Check.** Run `{{.Tool}} check` and fix every error until it reports `"ok": true`.
   The check also samples the picture with the captions hidden: it fails on
   caption-only stretches and on stretches where nothing changes. Fix warnings
   too unless the brief or `STYLE.md` calls for it.
{{if .Media.Library}}
{{template "media.md" .}}{{end}}
You may only run `{{.Tool}} ...` commands in the shell. Keep all files inside this
directory. When done, reply with two or three sentences describing the short.
