package production

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Article is a parsed article.md.
type Article struct {
	Title       string   `yaml:"title"`
	SEOTitle    string   `yaml:"seoTitle,omitempty"`
	Description string   `yaml:"description"`
	Slug        string   `yaml:"slug"`
	Tags        []string `yaml:"tags"`
	Body        string   `yaml:"-"`
	Words       int      `yaml:"-"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ParseArticle splits frontmatter from body and checks the fields the
// blog's content collection requires.
func ParseArticle(raw []byte) (Article, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return Article{}, errors.New("article.md must start with a --- frontmatter block")
	}
	end := bytes.Index(raw[4:], []byte("\n---"))
	if end < 0 {
		return Article{}, errors.New("article.md frontmatter is not closed with ---")
	}
	var a Article
	if err := yaml.Unmarshal(raw[4:4+end], &a); err != nil {
		return Article{}, fmt.Errorf("article.md frontmatter: %w", err)
	}
	a.Body = strings.TrimSpace(string(raw[4+end+4:]))
	a.Words = len(strings.Fields(a.Body))
	switch {
	case strings.TrimSpace(a.Title) == "":
		return a, errors.New("frontmatter title is required")
	case len([]rune(a.Description)) < 50 || len([]rune(a.Description)) > 200:
		return a, fmt.Errorf("frontmatter description must be 50–200 characters (has %d)", len([]rune(a.Description)))
	case !slugPattern.MatchString(a.Slug):
		return a, fmt.Errorf("frontmatter slug %q must be lowercase ASCII words joined by hyphens", a.Slug)
	case a.SEOTitle != "" && len([]rune(a.SEOTitle)) > 60:
		return a, errors.New("frontmatter seoTitle must be at most 60 characters")
	case len(a.Tags) == 0:
		return a, errors.New("frontmatter needs at least one tag")
	case strings.HasPrefix(a.Body, "# "):
		return a, errors.New("the body must not start with an H1; the title is the H1")
	case a.Words < 100:
		return a, errors.New("the body is too short")
	}
	return a, nil
}
