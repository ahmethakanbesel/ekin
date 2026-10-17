A reviewer read the script (revision {{.Revision}}) and asked for {{if eq .Count 1}}a change{{else}}{{.Count}} changes; address every one{{end}}:

{{.Notes}}
Files may have changed since your last turn (a reviewer can edit them directly),
so read the files you are about to change again first. Apply {{if eq .Count 1}}the note{{else}}each note{{end}} with the smallest change that fully addresses it, in
`script.md`, `narration/narration.txt` and `metadata.json` as needed. Keep what
the reviewer did not mention. This is still the script alone: do not run
`{{.Tool}} tts` or build any visuals. Reply with one or two sentences on what you
changed{{if gt .Count 1}}, note by note{{end}}.
