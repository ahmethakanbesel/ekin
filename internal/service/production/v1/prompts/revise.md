A reviewer watched revision {{.Revision}} and asked for {{if eq .Count 1}}a change{{else}}{{.Count}} changes; address every one{{end}}:

{{.Notes}}
Files may have changed since your last turn (a reviewer can edit them directly),
so read the files you are about to change again first. Apply {{if eq .Count 1}}the note{{else}}each note{{end}} with the smallest change that fully addresses it. Keep what
the reviewer did not mention. If the narration text changes, run the tts tool
again and re-time every cue. Look at the affected frames, then finish only when
`{{.Tool}} check` passes again. Reply with one or two sentences on what you changed{{if gt .Count 1}}, note by note{{end}}.
