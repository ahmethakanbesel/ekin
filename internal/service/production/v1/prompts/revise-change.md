A reviewer read revision {{.Revision}} of your change and asked for {{if eq .Count 1}}a change{{else}}{{.Count}} changes; address every one{{end}}:

{{.Notes}}
The repository in `repo/` still holds your change. Apply {{if eq .Count 1}}the note{{else}}each note{{end}} with the smallest edit that fully
addresses it and keep what the reviewer did not mention. Run `{{.Tool}} build` until it passes,
check the whole change with `{{.Tool}} diff`, and update `summary.md`. Reply with one or two
sentences on what you changed{{if gt .Count 1}}, note by note{{end}}.
