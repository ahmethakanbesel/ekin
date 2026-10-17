## Media sources

This video takes stock and music from the workspace's library as follows.
`{{.Tool}} library --type <music|video|photo> [--query "words"]` lists what fits this
video (least used first); `{{.Tool}} library use <id>` puts an item in place and adds its
credit to `assets/credits.json`.

{{range .Media.Sources -}}
- **{{.Label}}:**
{{- if eq .Mode "library"}} from the library only (`{{$.Tool}} library --type {{.Kind}}`).
  {{- if eq .Kind "music"}} Do not compose your own.{{else}} Do not search with `{{$.Tool}} stock`.{{end}}
{{- else if eq .Mode "mixed"}} from the library first (`{{$.Tool}} library --type {{.Kind}}`);
  {{- if eq .Kind "music"}} compose `music.js` only when nothing there fits.{{else}} search with `{{$.Tool}} stock --type {{.Kind}}` only when nothing there fits.{{end}}
{{- else}}
  {{- if eq .Kind "music"}} compose `music.js` as usual.{{else}} search with `{{$.Tool}} stock --type {{.Kind}}` as usual.{{end}}
{{- end}}
{{end}}
A library track is mixed under the voice from `music.json` (set `"offset"` there, in
seconds, to start later in it); a library score is a `music.js` like your own. Where a
kind comes from the library only and nothing there fits, do without it. Choose by the
descriptions and tags; when two fit equally, take the one used less.
