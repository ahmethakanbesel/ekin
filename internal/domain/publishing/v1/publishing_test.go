package publishing_test

import (
	"testing"

	publishing "github.com/ahmethakanbesel/ekin/internal/domain/publishing/v1"
)

func TestBlogTargetPostURL(t *testing.T) {
	tests := []struct {
		name          string
		defaultLocale string
		locale        string
		want          string
	}{
		{"english by default", "", "en", "https://example.com/blog/how-sleep-works/"},
		{"other locale gets a prefix", "", "tr", "https://example.com/tr/blog/how-sleep-works/"},
		{"turkish default", "tr", "tr", "https://example.com/blog/how-sleep-works/"},
		{"english beside a turkish default", "tr", "en", "https://example.com/en/blog/how-sleep-works/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := publishing.BlogTarget{Kind: publishing.BlogDir, Dir: "/srv/site", ContentDir: "src/content/blog", SiteURL: "https://example.com/", DefaultLocale: tt.defaultLocale}
			if err := b.Validate(); err != nil {
				t.Fatal(err)
			}
			if got := b.PostURL(tt.locale, "how-sleep-works"); got != tt.want {
				t.Fatalf("PostURL = %q, want %q", got, tt.want)
			}
		})
	}
}
