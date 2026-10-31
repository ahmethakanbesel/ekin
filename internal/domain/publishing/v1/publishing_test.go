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

func TestBlogTargetPublishMode(t *testing.T) {
	github := func(mode publishing.PublishMode) publishing.BlogTarget {
		return publishing.BlogTarget{Kind: publishing.BlogGitHub, Repo: "o/site", ContentDir: "src/content/blog", SealedToken: "sealed", Mode: mode}
	}
	dir := func(mode publishing.PublishMode) publishing.BlogTarget {
		return publishing.BlogTarget{Kind: publishing.BlogDir, Dir: "/srv/site", ContentDir: "src/content/blog", Mode: mode}
	}
	tests := []struct {
		name     string
		target   publishing.BlogTarget
		wantMode publishing.PublishMode
		wantPRs  bool
		wantErr  bool
	}{
		{name: "github defaults to commits", target: github(""), wantMode: publishing.PublishCommit},
		{name: "github commits", target: github(publishing.PublishCommit), wantMode: publishing.PublishCommit},
		{name: "github pull requests", target: github(publishing.PublishPullRequest), wantMode: publishing.PublishPullRequest, wantPRs: true},
		{name: "unknown mode", target: github("push"), wantErr: true},
		{name: "a local checkout has no mode", target: dir(publishing.PublishCommit), wantMode: ""},
		{name: "a local checkout cannot open pull requests", target: dir(publishing.PublishPullRequest), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.target
			err := b.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("accepted mode %q for %s", tt.target.Mode, tt.target.Kind)
				}
				return
			}
			if err != nil || b.Mode != tt.wantMode || b.PullRequests() != tt.wantPRs {
				t.Fatalf("validate = %v, mode %q, pull requests %v", err, b.Mode, b.PullRequests())
			}
		})
	}
}
