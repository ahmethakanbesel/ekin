// Package pexels searches Pexels for stock videos and photos and
// downloads a rendition sized for the target frame.
package pexels

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"
)

// Client calls the Pexels API.
type Client struct {
	APIKey  string
	BaseURL string // default https://api.pexels.com
	HTTP    *http.Client
}

// ErrDisabled means no API key is configured.
var ErrDisabled = errors.New("pexels: stock media is disabled (no KURGU_PEXELS_API_KEY)")

// Media is one search hit, with the rendition picked for the request.
type Media struct {
	ID          int     `json:"id"`
	Kind        string  `json:"kind"` // video | photo
	PageURL     string  `json:"pageUrl"`
	Author      string  `json:"author"`
	AuthorURL   string  `json:"authorUrl"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Duration    float64 `json:"duration,omitempty"`
	DownloadURL string  `json:"-"`
	// OriginalURL is the largest file Pexels offers: the one to fetch again
	// when a bigger or longer cut is needed.
	OriginalURL string `json:"-"`
	Alt         string `json:"alt,omitempty"`
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.pexels.com"
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Orientation is landscape, portrait or square.
type Orientation string

// Search finds up to count items of kind ("video" or "photo"), picking for
// each the smallest rendition at least minSide pixels on its short side.
func (c *Client) Search(ctx context.Context, kind, query string, orientation Orientation, count, minSide int) ([]Media, error) {
	if c.APIKey == "" {
		return nil, ErrDisabled
	}
	q := url.Values{"query": {query}, "per_page": {strconv.Itoa(max(count*3, 5))}}
	if orientation != "" {
		q.Set("orientation", string(orientation))
	}
	path := "/v1/search"
	if kind == "video" {
		path = "/videos/search"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.APIKey)
	res, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("pexels: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("pexels: search answered %d: %s", res.StatusCode, b)
	}
	var out []Media
	if kind == "video" {
		var body struct {
			Videos []struct {
				ID       int     `json:"id"`
				URL      string  `json:"url"`
				Width    int     `json:"width"`
				Height   int     `json:"height"`
				Duration float64 `json:"duration"`
				User     struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				} `json:"user"`
				Files []struct {
					Link     string `json:"link"`
					FileType string `json:"file_type"`
					Width    int    `json:"width"`
					Height   int    `json:"height"`
				} `json:"video_files"`
			} `json:"videos"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			return nil, fmt.Errorf("pexels: decode: %w", err)
		}
		for _, v := range body.Videos {
			files := v.Files
			sort.Slice(files, func(i, j int) bool { return files[i].Width*files[i].Height < files[j].Width*files[j].Height })
			link, w, h := "", 0, 0
			for _, f := range files {
				if f.FileType == "video/mp4" && min(f.Width, f.Height) >= minSide {
					link, w, h = f.Link, f.Width, f.Height
					break
				}
			}
			if link == "" && len(files) > 0 {
				f := files[len(files)-1]
				link, w, h = f.Link, f.Width, f.Height
			}
			if link == "" {
				continue
			}
			largest := files[len(files)-1].Link
			out = append(out, Media{ID: v.ID, Kind: "video", PageURL: v.URL, Author: v.User.Name, AuthorURL: v.User.URL,
				Width: w, Height: h, Duration: v.Duration, DownloadURL: link, OriginalURL: largest})
			if len(out) == count {
				break
			}
		}
		return out, nil
	}
	var body struct {
		Photos []struct {
			ID              int    `json:"id"`
			URL             string `json:"url"`
			Width           int    `json:"width"`
			Height          int    `json:"height"`
			Photographer    string `json:"photographer"`
			PhotographerURL string `json:"photographer_url"`
			Alt             string `json:"alt"`
			Src             struct {
				Original string `json:"original"`
				Large2x  string `json:"large2x"`
			} `json:"src"`
		} `json:"photos"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("pexels: decode: %w", err)
	}
	for _, p := range body.Photos {
		link := p.Src.Large2x
		if min(p.Width, p.Height) > 0 && minSide > 1200 {
			link = p.Src.Original
		}
		out = append(out, Media{ID: p.ID, Kind: "photo", PageURL: p.URL, Author: p.Photographer, AuthorURL: p.PhotographerURL,
			Width: p.Width, Height: p.Height, DownloadURL: link, OriginalURL: cmp.Or(p.Src.Original, link), Alt: p.Alt})
		if len(out) == count {
			break
		}
	}
	return out, nil
}

// Download saves m to path.
func (c *Client) Download(ctx context.Context, m Media, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.DownloadURL, nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("pexels: download: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("pexels: download answered %d", res.StatusCode)
	}
	f, err := os.Create(path) //nolint:gosec // caller's output path
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(res.Body, 500<<20)); err != nil {
		_ = f.Close()
		return fmt.Errorf("pexels: download: %w", err)
	}
	return f.Close()
}
