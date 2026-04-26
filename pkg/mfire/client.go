package mfire

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Defaults
const (
	DefaultTimeout    = 60 * time.Second
	DefaultRateLimit  = 500 * time.Millisecond // 2 requests per second
	DefaultMaxRetries = 3
)

// Client is an HTTP client for mangafire.to with built-in retry, rate
// limiting, and Cloudflare 403 handling.
type Client struct {
	http       *http.Client
	rateLimit  time.Duration
	maxRetries int

	mu       sync.Mutex
	lastCall time.Time
}

// NewClient creates a new Client with the given options.
func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		MaxIdleConns:         20,
		MaxIdleConnsPerHost:  10,
		IdleConnTimeout:      90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &Client{
		http: &http.Client{
			Transport: tr,
			Timeout:   DefaultTimeout,
			Jar:       jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		rateLimit:  DefaultRateLimit,
		maxRetries: DefaultMaxRetries,
	}
}

// SetRateLimit adjusts the delay between requests.
func (c *Client) SetRateLimit(d time.Duration) { c.rateLimit = d }

// SetMaxRetries adjusts the number of retry attempts on failure.
func (c *Client) SetMaxRetries(n int) { c.maxRetries = n }

// throttle ensures we don't exceed the rate limit.
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	elapsed := time.Since(c.lastCall)
	if elapsed < c.rateLimit {
		time.Sleep(c.rateLimit - elapsed)
	}
	c.lastCall = time.Now()
}

// newRequest creates an HTTP GET request with standard headers.
func (c *Client) newRequest(rawURL string) (*http.Request, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://mangafire.to/")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	return req, nil
}

// ---------------------------------------------------------------------------
// public fetch methods
// ---------------------------------------------------------------------------

// FetchDocument fetches a URL and returns a goquery document, with retry and
// 403 backoff.
func (c *Client) FetchDocument(rawURL string) (*goquery.Document, error) {
	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		c.throttle()

		req, err := c.newRequest(rawURL)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		req = req.WithContext(ctx)
		resp, err := c.http.Do(req)

		if err != nil {
			cancel()
			lastErr = fmt.Errorf("http do (attempt %d): %w", attempt+1, err)
			c.backoff(attempt)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel() // cancel AFTER body read — cancelling earlier invalidates the stream
		if readErr != nil {
			lastErr = fmt.Errorf("read body (attempt %d): %w", attempt+1, readErr)
			c.backoff(attempt)
			continue
		}

		switch {
		case resp.StatusCode == 429 || resp.StatusCode == 503:
			lastErr = fmt.Errorf("attempt %d: rate limited (%s)", attempt+1, resp.Status)
			c.backoff(attempt)
			continue
		case resp.StatusCode == 403:
			lastErr = fmt.Errorf("attempt %d: cloudflare 403", attempt+1)
			time.Sleep(time.Duration(20+rand.Intn(20)) * time.Second)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("attempt %d: bad status %s: %s", attempt+1, resp.Status, truncate(string(body), 200))
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("parse html: %w", err)
		}
		return doc, nil
	}
	return nil, fmt.Errorf("all %d attempts failed: %w", c.maxRetries, lastErr)
}

// FetchJSON fetches a URL and returns the raw response body with JSON headers.
func (c *Client) FetchJSON(rawURL string) ([]byte, error) {
	// We need different request headers for JSON. Reuse the retry loop but
	// use a wrapper that rebuilds the request with JSON headers.
	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		c.throttle()

		req, err := c.newRequest(rawURL)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		req = req.WithContext(ctx)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http do (attempt %d): %w", attempt+1, err)
			c.backoff(attempt)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read body (attempt %d): %w", attempt+1, readErr)
			c.backoff(attempt)
			continue
		}

		switch {
		case resp.StatusCode == 429 || resp.StatusCode == 503:
			lastErr = fmt.Errorf("attempt %d: rate limited (%s)", attempt+1, resp.Status)
			c.backoff(attempt)
			continue
		case resp.StatusCode == 403:
			lastErr = fmt.Errorf("attempt %d: cloudflare 403", attempt+1)
			time.Sleep(time.Duration(20+rand.Intn(20)) * time.Second)
			continue
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("attempt %d: bad status %s: %s", attempt+1, resp.Status, truncate(string(body), 200))
		}

		return body, nil
	}
	return nil, fmt.Errorf("all %d attempts failed: %w", c.maxRetries, lastErr)
}

// FetchDocumentWithVRF fetches a URL that requires a VRF token. It appends
// the vrf parameter using the provided keyword.
func (c *Client) FetchDocumentWithVRF(rawURL string, keyword string) (*goquery.Document, error) {
	vrfToken, err := VRF(keyword)
	if err != nil {
		return nil, fmt.Errorf("generate vrf: %w", err)
	}

	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	vrfURL := rawURL + sep + "vrf=" + url.QueryEscape(vrfToken)

	return c.FetchDocument(vrfURL)
}

// backoff sleeps with exponential backoff + jitter. The first wait is ~2 s,
// second is ~4 s. The third attempt has no backoff since it's the last.
func (c *Client) backoff(attempt int) {
	if attempt >= c.maxRetries-1 {
		return
	}
	base := time.Duration(2*math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(rand.Intn(2000)) * time.Millisecond
	time.Sleep(base + jitter)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ResolveURL resolves a potentially relative URL against the base.
func ResolveURL(href string) string {
	if href == "" {
		return ""
	}
	if parsed, err := url.Parse(href); err == nil && parsed.IsAbs() {
		return href
	}
	base, _ := url.Parse("https://mangafire.to")
	resolved, err := base.Parse(href)
	if err != nil {
		return href
	}
	return resolved.String()
}

// ExtractSlug extracts the manga slug from a /manga/ URL path.
func ExtractSlug(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p == "manga" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
