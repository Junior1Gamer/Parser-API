package mfire

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const mangaBaseURL = "https://mangafire.to/manga/"

// DetailWriter is the interface the parallel fetcher uses to persist each
// manga detail as soon as it is fetched. Implementations should be safe for
// concurrent use.
type DetailWriter interface {
	WriteDetail(detail MangaDetail) error
	DetailExists(slug string) bool
}

// FetchMangaDetail scrapes the full metadata for a single manga, including
// its chapter list.
func (c *Client) FetchMangaDetail(slug string) (*MangaDetail, error) {
	pageURL := mangaBaseURL + slug

	doc, err := c.FetchDocument(pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetch detail %s: %w", slug, err)
	}

	result := &MangaDetail{
		Slug: slug,
	}

	// --- Title ---
	result.Title = strings.TrimSpace(doc.Find("h1[itemprop=name]").First().Text())

	// Guard: a valid manga page must have a title. If the page is a
	// Cloudflare challenge or error page, skip early.
	if result.Title == "" {
		// Try OG meta fallback.
		doc.Find("meta[property='og:title']").Each(func(_ int, s *goquery.Selection) {
			if c, ok := s.Attr("content"); ok {
				result.Title = strings.TrimSpace(c)
			}
		})
	}
	// If still empty, this is likely not a real manga page.
	if result.Title == "" {
		// One last attempt: generic h1.
		result.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if result.Title == "" {
		// Check for known "not found" indicators.
		bodyText := strings.TrimSpace(doc.Find("body").Text())
		if strings.Contains(bodyText, "404") || strings.Contains(bodyText, "not found") || strings.Contains(bodyText, "Page not") {
			return nil, fmt.Errorf("page appears to be an error/not-found page")
		}
		return nil, fmt.Errorf("page has no title — likely not a valid manga detail page")
	}

	// --- Cover image ---
	doc.Find("meta[property='og:image']").Each(func(_ int, s *goquery.Selection) {
		if c, ok := s.Attr("content"); ok {
			result.Cover = c
		}
	})
	if result.Cover == "" {
		doc.Find(".poster img, .cover img, img[itemprop=image]").Each(func(_ int, s *goquery.Selection) {
			if src, ok := s.Attr("src"); ok {
				result.Cover = ResolveURL(src)
			}
		})
	}

	// --- Description ---
	doc.Find("meta[name=description], meta[property='og:description']").Each(func(_ int, s *goquery.Selection) {
		if c, ok := s.Attr("content"); ok {
			desc := strings.TrimSpace(c)
			if len(desc) > 50 && !strings.Contains(desc, "read manga") {
				result.Description = desc
			}
		}
	})
	if result.Description == "" {
		desc := strings.TrimSpace(doc.Find("div.description, div.synopsis, .description, .synopsis").First().Text())
		if len(desc) > 50 {
			result.Description = desc
		}
	}

	// --- Status ---
	result.Status = strings.TrimSpace(doc.Find(".info p, .meta p, .status").First().Text())

	// --- Genres ---
	doc.Find("a[href*='/genre/']").Each(func(_ int, s *goquery.Selection) {
		g := strings.TrimSpace(s.Text())
		if g != "" {
			result.Genres = append(result.Genres, g)
		}
	})
	if len(result.Genres) == 0 {
		doc.Find(".genres a, .tags a, .info-rating .meta a").Each(func(_ int, s *goquery.Selection) {
			g := strings.TrimSpace(s.Text())
			if g != "" {
				result.Genres = append(result.Genres, g)
			}
		})
	}

	// --- Alternative titles ---
	doc.Find(".alt-title, .alternative, h2:contains('Alternative'), .other-names").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		text = strings.TrimPrefix(text, "Alternative:")
		text = strings.TrimPrefix(text, "Alt:")
		text = strings.TrimSpace(text)
		if text != "" {
			parts := strings.Split(text, ";")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result.AltTitles = append(result.AltTitles, p)
				}
			}
		}
	})

	// --- Updated at ---
	doc.Find(".update, time, .meta time, .info time").Each(func(_ int, s *goquery.Selection) {
		if dt, ok := s.Attr("datetime"); ok {
			result.UpdatedAt = strings.TrimSpace(dt)
		} else {
			t := strings.TrimSpace(s.Text())
			if t != "" && result.UpdatedAt == "" {
				result.UpdatedAt = t
			}
		}
	})

	// --- Chapters ---
	result.Chapters = parseChapters(doc)

	return result, nil
}

// parseChapters extracts the chapter list from a manga detail page.
func parseChapters(doc *goquery.Document) []Chapter {
	var chapters []Chapter

	doc.Find("ul li[data-number], .list-body li, .chapters li, .chapter-list li").Each(func(_ int, s *goquery.Selection) {
		ch := Chapter{}
		if numStr, ok := s.Attr("data-number"); ok {
			ch.Number = strings.TrimSpace(numStr)
		}
		link := s.Find("a").First()
		ch.URL, _ = link.Attr("href")
		ch.URL = ResolveURL(ch.URL)
		ch.Title = strings.TrimSpace(link.AttrOr("title", ""))
		if ch.Title == "" {
			ch.Title = strings.TrimSpace(link.Text())
		}
		if ch.Number == "" {
			href := link.AttrOr("href", "")
			parts := strings.Split(href, "/")
			for _, p := range parts {
				if isChapterNumber(p) {
					ch.Number = p
					break
				}
			}
		}
		ch.Date = strings.TrimSpace(s.Find("span.date, .chapter-date, time").First().Text())
		if ch.Number != "" || ch.Title != "" {
			chapters = append(chapters, ch)
		}
	})

	if len(chapters) == 0 {
		doc.Find("a[href*='/chapter/']").Each(func(_ int, s *goquery.Selection) {
			ch := Chapter{}
			ch.URL, _ = s.Attr("href")
			ch.URL = ResolveURL(ch.URL)
			ch.Title = strings.TrimSpace(s.Text())
			parts := strings.Split(ch.URL, "/")
			for _, p := range parts {
				if isChapterNumber(p) {
					ch.Number = p
					break
				}
			}
			if ch.Number != "" || ch.Title != "" {
				chapters = append(chapters, ch)
			}
		})
	}

	return chapters
}

// isChapterNumber reports whether s looks like a chapter number.
func isChapterNumber(s string) bool {
	s = strings.TrimPrefix(s, "ch")
	s = strings.TrimPrefix(s, "chapter-")
	s = strings.TrimPrefix(s, "chapter.")
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	dot := false
	for _, r := range s {
		if r == '.' {
			if dot {
				return false
			}
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Sequential detail fetch (kept for completeness / small batches)
// ---------------------------------------------------------------------------

// FetchAllMangaDetails sequentially fetches details for all slugs. Progress
// messages are sent on the channel when non-nil.
func (c *Client) FetchAllMangaDetails(slugs []string, progress chan<- string) ([]MangaDetail, error) {
	var details []MangaDetail
	for i, slug := range slugs {
		detail, err := c.FetchMangaDetail(slug)
		if err != nil {
			log.Printf("Warning: detail failed for %s: %v; skipping", slug, err)
			continue
		}
		details = append(details, *detail)
		if progress != nil {
			progress <- fmt.Sprintf("detail %d/%d: %s", i+1, len(slugs), slug)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return details, nil
}

// ---------------------------------------------------------------------------
// Parallel detail fetch with resume support
// ---------------------------------------------------------------------------

// FetchAllMangaDetailsParallel fetches manga details concurrently using the
// given number of workers. It respects a shared rate limiter, writes each
// detail immediately via the provided writer, and skips slugs whose detail
// file already exists (resume support).
//
// Returns the number of new details fetched (not including resumed skips).
func (c *Client) FetchAllMangaDetailsParallel(
	slugs []string,
	workers int,
	ratePerSec int,
	writer DetailWriter,
	progress chan<- string,
) (int, error) {

	if workers < 1 {
		workers = 1
	}
	if ratePerSec < 1 {
		ratePerSec = 1
	}

	// Build work queue, skipping already-fetched slugs.
	var totalSkipped int64
	work := make(chan string, len(slugs))
	for _, slug := range slugs {
		if writer != nil && writer.DetailExists(slug) {
			atomic.AddInt64(&totalSkipped, 1)
			continue
		}
		work <- slug
	}
	close(work)

	total := len(work)
	if total == 0 {
		log.Printf("All %d manga already have detail files; nothing to do.", len(slugs))
		return 0, nil
	}

	log.Printf("Starting parallel detail fetch: %d new, %d already done, %d workers, %d req/s",
		total, totalSkipped, workers, ratePerSec)

	limiter := NewSharedRateLimiter(ratePerSec)
	defer limiter.Close()

	var (
		wg       sync.WaitGroup
		fetched  int64
		totalMu  sync.Mutex
		allErrs  []error
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for slug := range work {
				limiter.Acquire()

				detail, err := c.FetchMangaDetail(slug)
				if err != nil {
					errMsg := fmt.Errorf("worker %d: detail %s: %w", workerID, slug, err)
					log.Printf("Warning: %v", errMsg)
					totalMu.Lock()
					allErrs = append(allErrs, errMsg)
					totalMu.Unlock()
					continue
				}

				// Write immediately.
				if writer != nil {
					if we := writer.WriteDetail(*detail); we != nil {
						log.Printf("Warning: write %s: %v", slug, we)
					}
				}

				n := atomic.AddInt64(&fetched, 1)
				if progress != nil {
					progress <- fmt.Sprintf("detail %d/%d (worker %d): %s", n, total, workerID, slug)
				}
			}
		}(w)
	}

	wg.Wait()

	totalFetched := int(atomic.LoadInt64(&fetched))
	log.Printf("Parallel detail fetch complete: %d new, %d skipped, %d errors",
		totalFetched, totalSkipped, len(allErrs))

	return totalFetched, nil
}

// DirectDetailWriter is a DetailWriter that writes JSON files to a base
// directory. Safe for concurrent use.
type DirectDetailWriter struct {
	BaseDir string
}

// NewDirectDetailWriter creates a writer that stores manga detail JSON files
// under baseDir/manga/.
func NewDirectDetailWriter(baseDir string) *DirectDetailWriter {
	return &DirectDetailWriter{BaseDir: baseDir}
}

// DetailExists returns true when the detail file for the given slug already
// exists on disk.
func (w *DirectDetailWriter) DetailExists(slug string) bool {
	p := filepath.Join(w.BaseDir, "manga", slug+".json")
	_, err := os.Stat(p)
	return err == nil
}

// WriteDetail marshals the detail to indented JSON and writes it atomically
// to baseDir/manga/{slug}.json via a temp file + rename. This prevents
// partial/corrupt files if the process is killed mid-write.
func (w *DirectDetailWriter) WriteDetail(detail MangaDetail) error {
	dir := filepath.Join(w.BaseDir, "manga")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	p := filepath.Join(dir, detail.Slug+".json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
