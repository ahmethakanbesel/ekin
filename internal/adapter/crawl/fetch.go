package crawl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrPrivateAddress is a fetch refused because the host resolves to a
// loopback, private or otherwise internal address.
var ErrPrivateAddress = errors.New("refusing to fetch an internal address")

var internalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
}

// internal reports whether ip is not a public unicast address.
func internal(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range internalPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// newClient returns the HTTP client the crawler fetches with: redirects
// are returned, not followed (each hop is a page of its own), no proxy
// from the environment, and, unless allowPrivate, a dialer that refuses
// internal addresses after resolution so a DNS answer cannot point it
// at the host's own network.
func newClient(timeout time.Duration, allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || internal(ip) {
				return fmt.Errorf("%w (%s)", ErrPrivateAddress, host)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: timeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type response struct {
	status   int
	header   http.Header
	body     []byte
	location string // raw Location of a redirect
}

// get fetches rawURL and, when it answers 200 and read approves its
// headers, reads at most maxBody bytes of the body.
func (c *Crawler) get(ctx context.Context, rawURL string, read func(http.Header) bool, maxBody int64) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.5")
	res, err := c.client.Do(req) //nolint:gosec // fetching the site is the point; the dialer refuses internal addresses
	if err != nil {
		return response{}, err
	}
	defer func() { _ = res.Body.Close() }()
	out := response{status: res.StatusCode, header: res.Header, location: res.Header.Get("Location")}
	if res.StatusCode == http.StatusOK && read(res.Header) {
		out.body, err = io.ReadAll(io.LimitReader(res.Body, maxBody))
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
