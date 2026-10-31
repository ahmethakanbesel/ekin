package crawl

import (
	"net/url"
	"path"
	"strings"
)

// Normalize resolves ref against base and returns it in the form pages
// are keyed by: http(s) only, scheme and host lowercased, default port
// and fragment dropped, an empty path read as "/". ok is false for
// anything else (mailto:, javascript:, malformed).
func Normalize(base *url.URL, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") {
		return "", false
	}
	u, err := base.Parse(ref)
	if err != nil {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && (u.Scheme != "http" || p != "80") && (u.Scheme != "https" || p != "443") {
		host += ":" + p
	}
	u.Host = host
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path, u.RawPath = "/", ""
	}
	return u.String(), true
}

// sameSite reports whether u is on root's host, counting the host with
// and without "www." as one site and either scheme.
func sameSite(root, u *url.URL) bool {
	strip := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }
	return strip(root.Host) == strip(u.Host)
}

// assetExt are extensions of files that are not pages; links to them
// are not crawled.
var assetExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true, ".svg": true, ".ico": true,
	".bmp": true, ".tif": true, ".tiff": true, ".pdf": true, ".zip": true, ".gz": true, ".tgz": true, ".rar": true,
	".7z": true, ".mp4": true, ".mov": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true, ".m4a": true,
	".css": true, ".js": true, ".mjs": true, ".map": true, ".json": true, ".xml": true, ".txt": true, ".csv": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true, ".doc": true, ".docx": true,
	".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true, ".rss": true, ".atom": true, ".dmg": true, ".exe": true,
}

func isAsset(u *url.URL) bool { return assetExt[strings.ToLower(path.Ext(u.Path))] }

// requestPath is what robots.txt rules match: the path and query.
func requestPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	return p
}
