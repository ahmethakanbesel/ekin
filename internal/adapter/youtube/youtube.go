// Package youtube connects a channel with OAuth 2.0 and uploads videos
// through the YouTube Data API v3 resumable upload protocol.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Endpoints are overridable for tests.
type Endpoints struct {
	Auth, Token, API, Upload string
}

// Google is the production endpoint set.
var Google = Endpoints{
	Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
	Token:  "https://oauth2.googleapis.com/token",
	API:    "https://www.googleapis.com/youtube/v3",
	Upload: "https://www.googleapis.com/upload/youtube/v3/videos",
}

// Scopes requested: upload, and read the channel to show which one is
// connected.
const scopes = "https://www.googleapis.com/auth/youtube.upload https://www.googleapis.com/auth/youtube.readonly"

// Client is an OAuth client for one Google Cloud project.
type Client struct {
	ClientID, ClientSecret, RedirectURL string
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
	return &http.Client{Timeout: 0} // uploads can take a while; contexts bound them
}

// AuthURL is where the browser goes to grant access. offline + consent
// guarantee a refresh token.
func (c *Client) AuthURL(state string) string {
	q := url.Values{
		"client_id": {c.ClientID}, "redirect_uri": {c.RedirectURL}, "response_type": {"code"},
		"scope": {scopes}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state},
		"include_granted_scopes": {"true"},
	}
	return c.ep().Auth + "?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
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
	res, err := c.http().Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("youtube: token: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var t tokenResponse
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t)
	if res.StatusCode != http.StatusOK || t.AccessToken == "" {
		return t, fmt.Errorf("youtube: token request failed: %s %s", t.Error, t.Description)
	}
	return t, nil
}

// Exchange trades the authorisation code for tokens and returns the
// refresh token and a fresh access token.
func (c *Client) Exchange(ctx context.Context, code string) (refresh, access string, err error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.RedirectURL}})
	if err != nil {
		return "", "", err
	}
	if t.RefreshToken == "" {
		return "", "", errors.New("youtube: Google returned no refresh token; remove Kurgu's access in your Google account and connect again")
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

// Channel returns the id and title of the authorised channel.
func (c *Client) Channel(ctx context.Context, access string) (id, title string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ep().API+"/channels?part=snippet&mine=true", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	res, err := c.http().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("youtube: channels: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return "", "", fmt.Errorf("youtube: channels answered %d: %s", res.StatusCode, b)
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || len(body.Items) == 0 {
		return "", "", errors.New("youtube: this Google account has no YouTube channel")
	}
	return body.Items[0].ID, body.Items[0].Snippet.Title, nil
}

// Video is what to upload.
type Video struct {
	Title, Description string
	Tags               []string
	Language           string // tr | en
	Privacy            string // private | unlisted | public
	CategoryID         string // default 27 (Education)
	Body               io.Reader
	Size               int64
}

// Upload sends v with the resumable protocol and returns the video id.
func (c *Client) Upload(ctx context.Context, access string, v Video) (string, error) {
	category := v.CategoryID
	if category == "" {
		category = "27"
	}
	meta, _ := json.Marshal(map[string]any{
		"snippet": map[string]any{
			"title": v.Title, "description": v.Description, "tags": v.Tags, "categoryId": category,
			"defaultLanguage": v.Language, "defaultAudioLanguage": v.Language,
		},
		"status": map[string]any{
			"privacyStatus": v.Privacy, "selfDeclaredMadeForKids": false, "containsSyntheticMedia": true,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.ep().Upload+"?uploadType=resumable&part=snippet,status&notifySubscribers=true", bytes.NewReader(meta))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "video/mp4")
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(v.Size, 10))
	res, err := c.http().Do(req)
	if err != nil {
		return "", fmt.Errorf("youtube: start upload: %w", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Location") == "" {
		return "", fmt.Errorf("youtube: start upload answered %d (check the upload quota and that the API is enabled)", res.StatusCode)
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, res.Header.Get("Location"), v.Body)
	if err != nil {
		return "", err
	}
	put.ContentLength = v.Size
	put.Header.Set("Content-Type", "video/mp4")
	res, err = c.http().Do(put)
	if err != nil {
		return "", fmt.Errorf("youtube: upload: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("youtube: upload answered %d: %s", res.StatusCode, firstN(string(raw), 300))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
		return "", fmt.Errorf("youtube: upload response has no video id: %s", firstN(string(raw), 300))
	}
	return out.ID, nil
}

// Thumbnail sets a custom thumbnail. Channels that are not verified for
// custom thumbnails get an error, which callers may ignore.
func (c *Client) Thumbnail(ctx context.Context, access, videoID string, jpeg io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.Replace(c.ep().Upload, "/videos", "/thumbnails/set", 1)+"?videoId="+url.QueryEscape(videoID), jpeg)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "image/jpeg")
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("youtube: thumbnail answered %d", res.StatusCode)
	}
	return nil
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
