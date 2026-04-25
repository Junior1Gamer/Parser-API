package mfire

import (
	"fmt"
	"io"
	"math"
	"math/rand"
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
	DefaultTimeout    = 30 * time.Second
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
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		http: &http.Client{
			Transport: tr,
			Timeout:   DefaultTimeout,
			Jar:       jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 5 redirects
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
func (c *Client) SetRateLimit(d time.Duration) {
	c.rateLimit = d
}

// SetMaxRetries adjusts the number of retry attempts on failure.
func (c *Client) SetMaxRetries(n int) {
	c.maxRetries = n
}

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

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http do (attempt %d): %w", attempt+1, err)
			c.backoff(attempt)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read body (attempt %d): %w", attempt+1, err)
			c.backoff(attempt)
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			lastErr = fmt.Errorf("rate limited (attempt %d): %s", attempt+1, resp.Status)
			c.backoff(attempt)
			continue
		}

		if resp.StatusCode == 403 {
			// Cloudflare challenge. Back off longer.
			lastErr = fmt.Errorf("cloudflare 403 (attempt %d)", attempt+1)
			// For 403, sleep 15-30 seconds before retry
			time.Sleep(time.Duration(15+rand.Intn(16)) * time.Second)
			continue
		}

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("bad status: %s (body: %s)", resp.Status, truncate(string(body), 200))
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("parse html: %w", err)
		}
		return doc, nil
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

	// Append vrf parameter
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	vrfURL := rawURL + sep + "vrf=" + url.QueryEscape(vrfToken)

	return c.FetchDocument(vrfURL)
}

// backoff sleeps with exponential backoff + jitter.
func (c *Client) backoff(attempt int) {
	if attempt >= c.maxRetries-1 {
		return
	}
	base := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
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
