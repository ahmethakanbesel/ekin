package openseo

import "context"

// Account is the connected account (whoami; no credits).
type Account struct {
	UserEmail        string   `json:"userEmail"`
	Mode             string   `json:"mode"`
	Scopes           []string `json:"scopes"`
	CreditsRemaining *float64 `json:"creditsRemaining"`
}

// Whoami confirms the key and returns the credit balance.
func (c *Client) Whoami(ctx context.Context) (Account, error) {
	var a Account
	return a, c.Call(ctx, "whoami", nil, &a)
}

// Project is an OpenSEO project; tools are scoped to one.
type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	LocationCode int    `json:"locationCode"`
	LanguageCode string `json:"languageCode"`
	URL          string `json:"url"`
	Organization string `json:"organization"`
}

// Projects lists the account's projects (no credits).
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	return out.Projects, c.Call(ctx, "list_projects", nil, &out)
}

// Keyword is one keyword row; nil metrics are unavailable.
type Keyword struct {
	Keyword           string   `json:"keyword"`
	SearchVolume      *int     `json:"searchVolume"`
	KeywordDifficulty *int     `json:"keywordDifficulty"`
	Intent            string   `json:"intent"`
	CPC               *float64 `json:"cpc"`
	Position          *int     `json:"position"` // domain suggestions only
}

// Market narrows a call to a country and language; zero values use the
// project's default market.
type Market struct {
	LocationCode int
	LanguageCode string
}

// SeedResult is research_keywords' result for one seed.
type SeedResult struct {
	Seed  string    `json:"seed"`
	OK    bool      `json:"ok"`
	Error string    `json:"error"`
	Rows  []Keyword `json:"rows"`
}

// Credit estimates from the tool descriptions, for the default limits.
const (
	CreditsPerSeed       = 54
	CreditsPerCompetitor = 300 // upper end of the typical 100–300
	MaxSeeds             = 5
)

// ResearchKeywords researches up to 5 seeds (about 54 credits each).
func (c *Client) ResearchKeywords(ctx context.Context, projectID string, seeds []string, m Market) ([]SeedResult, error) {
	items := make([]map[string]any, 0, len(seeds))
	for _, s := range seeds {
		item := map[string]any{"seed": s}
		if m.LocationCode != 0 {
			item["locationCode"] = m.LocationCode
		}
		if m.LanguageCode != "" {
			item["languageCode"] = m.LanguageCode
		}
		items = append(items, item)
	}
	var out struct {
		Results []SeedResult `json:"results"`
	}
	return out.Results, c.Call(ctx, "research_keywords", map[string]any{"projectId": projectID, "seeds": items, "resultLimit": 150}, &out)
}

// DomainKeywords lists the keywords a domain ranks for (about 100–300
// credits; cached 12 hours).
func (c *Client) DomainKeywords(ctx context.Context, projectID, domain string, m Market) ([]Keyword, error) {
	args := map[string]any{"projectId": projectID, "domain": domain}
	if m.LocationCode != 0 {
		args["locationCode"] = m.LocationCode
	}
	if m.LanguageCode != "" {
		args["languageCode"] = m.LanguageCode
	}
	var out struct {
		Keywords []Keyword `json:"keywords"`
	}
	return out.Keywords, c.Call(ctx, "get_domain_keyword_suggestions", args, &out)
}

// Locations maps common country codes to DataForSEO location codes.
var Locations = map[string]int{
	"TR": 2792, "US": 2840, "GB": 2826, "DE": 2276, "FR": 2250, "NL": 2528, "CA": 2124, "AU": 2036,
	"IE": 2372, "AZ": 2031, "CY": 2196, "AT": 2040, "CH": 2756, "BE": 2056, "SE": 2752, "ES": 2724, "IT": 2380,
}
