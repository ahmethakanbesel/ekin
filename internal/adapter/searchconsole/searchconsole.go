// Package searchconsole reads Google Search Console with OAuth 2.0
// (the read-only webmasters scope): the properties a person can see,
// and search analytics rows (clicks, impressions, position) per day and
// page or query.
package searchconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Endpoints are overridable for tests.
type Endpoints struct {
	Auth, Token, API string
}

// Google is the production endpoint set.
var Google = Endpoints{ //nolint:gosec // endpoint URLs, not credentials
	Auth:  "https://accounts.google.com/o/oauth2/v2/auth",
	Token: "https://oauth2.googleapis.com/token",
	API:   "https://www.googleapis.com/webmasters/v3",
}

// Scope is read-only access to Search Console data.
const Scope = "https://www.googleapis.com/auth/webmasters.readonly"

// Client is an OAuth client for one Google Cloud project (the one
// YouTube uses).
type Client struct {
	ClientID, ClientSecret, RedirectURL string //nolint:gosec // never serialised
	Endpoints                           Endpoints
	HTTP                                *http.Client
}

// Configured reports whether OAuth credentials are set.
func (c *Client) Configured() bool { return c != nil && c.ClientID != "" && c.ClientSecret != "" }

func (c *Client) ep() Endpoints {
	if c.Endpoints.Auth == "" {
		return Google
	}
	return c.Endpoints
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// AuthURL is where the browser goes to grant access; offline + consent
// guarantee a refresh token.
func (c *Client) AuthURL(state string) string {
	q := url.Values{
		"client_id": {c.ClientID}, "redirect_uri": {c.RedirectURL}, "response_type": {"code"},
		"scope": {Scope}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state},
	}
	return c.ep().Auth + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`  //nolint:gosec // Google's token response
	RefreshToken string `json:"refresh_token"` //nolint:gosec // Google's token response
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (c *Client) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep().Token, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http().Do(req) //nolint:gosec // the configured Google token endpoint
	if err != nil {
		return tokenResponse{}, fmt.Errorf("searchconsole: token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var t tokenResponse
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t)
	if res.StatusCode != http.StatusOK || t.AccessToken == "" {
		return t, fmt.Errorf("searchconsole: token request failed: %s %s", t.Error, t.Description)
	}
	return t, nil
}

// Exchange trades the authorisation code for a refresh token and a
// fresh access token.
func (c *Client) Exchange(ctx context.Context, code string) (refresh, access string, err error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.RedirectURL}})
	if err != nil {
		return "", "", err
	}
	if t.RefreshToken == "" {
		return "", "", errors.New("searchconsole: Google returned no refresh token; remove Ekin's access in your Google account and connect again")
	}
	return t.RefreshToken, t.AccessToken, nil
}

// Access returns a short-lived access token for refresh.
func (c *Client) Access(ctx context.Context, refresh string) (string, error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// Property is a Search Console property: a URL-prefix one
// ("https://example.com/") or a domain one ("sc-domain:example.com").
type Property struct {
	URL        string `json:"siteUrl"`
	Permission string `json:"permissionLevel"` // siteOwner, siteFullUser, siteRestrictedUser, siteUnverifiedUser
}

// Properties lists the properties the account can read.
func (c *Client) Properties(ctx context.Context, access string) ([]Property, error) {
	var out struct {
		Sites []Property `json:"siteEntry"`
	}
	if err := c.do(ctx, access, http.MethodGet, "/sites", nil, &out); err != nil {
		return nil, err
	}
	var ok []Property
	for _, p := range out.Sites {
		if p.Permission != "siteUnverifiedUser" {
			ok = append(ok, p)
		}
	}
	return ok, nil
}

// Query asks for search analytics rows.
type Query struct {
	Start, End time.Time // dates, inclusive
	// Dimensions are the row keys, e.g. ["date", "page"].
	Dimensions []string
	RowLimit   int // at most 25000
	StartRow   int
}

// Row is one row: its keys in the order of the dimensions, and its
// metrics.
type Row struct {
	Keys        []string `json:"keys"`
	Clicks      float64  `json:"clicks"`
	Impressions float64  `json:"impressions"`
	CTR         float64  `json:"ctr"`
	Position    float64  `json:"position"`
}

// Analytics runs q against property, web search only.
func (c *Client) Analytics(ctx context.Context, access, property string, q Query) ([]Row, error) {
	body := map[string]any{
		"startDate": q.Start.Format(time.DateOnly), "endDate": q.End.Format(time.DateOnly),
		"dimensions": q.Dimensions, "rowLimit": q.RowLimit, "startRow": q.StartRow, "type": "web",
		"dataState": "all",
	}
	var out struct {
		Rows []Row `json:"rows"`
	}
	path := "/sites/" + url.PathEscape(property) + "/searchAnalytics/query"
	if err := c.do(ctx, access, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return out.Rows, nil
}

// Error is an answer from the API outside 2xx.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("searchconsole: answered %d: %s", e.Status, e.Message)
}

func (c *Client) do(ctx context.Context, access, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.ep().API+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(req) //nolint:gosec // the configured Google API
	if err != nil {
		return fmt.Errorf("searchconsole: %s: %w", method, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return &Error{Status: res.StatusCode, Message: e.Error.Message}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
