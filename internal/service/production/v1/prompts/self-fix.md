An editor reviewed your draft (self-review round {{.Round}} of at most {{.Max}})
and asks for these changes:
{{range .Issues}}
- {{.File}}{{if .Passage}} ("{{.Passage}}"){{end}}: {{.Problem}} Fix: {{.Fix}}{{end}}

Make each change with the smallest edit that fully addresses it, and keep
everything else. If an item is mistaken (the editor misread a source, say),
leave it and say why in your reply.{{if .Video}} If the narration text changes,
run the tts tool again and re-time every cue, look at the affected frames, and
finish only when `{{.Tool}} check` passes again.{{end}} Reply with one or two
sentences on what you changed.
