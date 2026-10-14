A reviewer watched revision {{.Revision}} and asked for changes{{if .Scope}} to the {{.Scope}}{{end}}{{if .At}} at about {{.At}} s{{end}}:

> {{.Feedback}}

Files may have changed since your last turn (a reviewer can edit them directly),
so read the files you are about to change again first. Apply the feedback with the smallest change that fully addresses it. Keep what
the reviewer did not mention. If the narration text changes, run the tts tool
again and re-time every cue. Look at the affected frames, then finish only when
`{{.Tool}} check` passes again. Reply with one or two sentences on what you changed.
