package production

import (
	"strings"
	"testing"

	librarydomain "ekin/internal/domain/library/v1"
)

func TestPromptsNameEachMediaSource(t *testing.T) {
	base := func(policy librarydomain.Policy) map[string]any {
		return map[string]any{
			"LanguageName": "Turkish", "Tool": toolCmd, "Width": 1080, "Height": 1920, "MinSeconds": 15, "MaxSeconds": 60,
			"WordsPerSecond": "2.3", "TargetWords": 46, "Media": mediaModes(policy),
			"ClipStart": "0:10", "ClipEnd": "0:40", "ClipSeconds": "30", "TailSeconds": 3,
		}
	}
	tests := []struct {
		name    string
		page    string
		policy  librarydomain.Policy
		want    []string
		notWant []string
	}{
		{
			name:    "fresh reads as before",
			page:    "video.md",
			want:    []string{"Stock footage is optional", "write `music.js`", "`music.meta.json`"},
			notWant: []string{"Media sources", "library"},
		},
		{
			name:   "library music, mixed video, fresh photos",
			page:   "video.md",
			policy: librarydomain.Policy{Music: librarydomain.ModeLibrary, Video: librarydomain.ModeMixed},
			want: []string{
				"## Media sources",
				"**Video clips:** from the library first (`ekin tool library --type video`); search with `ekin tool stock --type video` only when nothing there fits.",
				"**Photos:** search with `ekin tool stock --type photo` as usual.",
				"**Music:** from the library only (`ekin tool library --type music`). Do not compose your own.",
				"add music\n   as **Media sources** below says",
			},
			notWant: []string{"Stock footage is optional"},
		},
		{
			name:   "a short of a library video",
			page:   "clip.md",
			policy: librarydomain.Policy{Photo: librarydomain.ModeLibrary},
			want:   []string{"## Media sources", "**Photos:** from the library only (`ekin tool library --type photo`). Do not search with `ekin tool stock`."},
		},
		{
			name:   "the kit names the library",
			page:   "kit.md",
			policy: librarydomain.Policy{Music: librarydomain.ModeMixed},
			want:   []string{"or `ekin tool library use`"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := execute(tt.page, base(tt.policy))
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q", w)
				}
			}
		})
	}
}
