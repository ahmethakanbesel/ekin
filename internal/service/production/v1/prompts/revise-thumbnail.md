A reviewer looked at the thumbnails of revision {{.Revision}} and asked for {{if eq .Count 1}}a change{{else}}{{.Count}} changes; address every one{{end}}:

{{.Notes}}
Change only `thumbnail.html`, `thumbnails.json` and the stills in
`thumbnails/stills/` (`{{.Tool}} thumbnail --stills ...` renders new ones). The
video itself is approved as it is: leave `composition.html`, the narration,
the music, `script.md` and `metadata.json` untouched, because the video is not
rendered again. Read `thumbnails.json` and `thumbnail.html` again first; a
reviewer may have changed the words. Then run `{{.Tool}} thumbnail`, read the
`.small.jpg` files, and finish only when it passes. Reply with one or two
sentences on what you changed{{if gt .Count 1}}, note by note{{end}}.
