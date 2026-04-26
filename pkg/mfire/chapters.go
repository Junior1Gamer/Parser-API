package mfire

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	ajaxReadBase = "https://mangafire.to/ajax/read"
	defaultType  = "chapter"
	defaultLang  = "en"
)

// FetchChapterPages fetches the page image URLs for a single chapter.
//
// The call goes to:
//
//	GET /ajax/read/{type}/{chapterId}?vrf={vrfToken}
//
// where the VRF token is computed from "{type}@{chapterId}".
func (c *Client) FetchChapterPages(chapterID, chapterType string) ([]PageImage, error) {
	if chapterType == "" {
		chapterType = defaultType
	}

	vrfInput := chapterType + "@" + chapterID
	vrfToken, err := VRF(vrfInput)
	if err != nil {
		return nil, fmt.Errorf("vrf(%q): %w", vrfInput, err)
	}

	ajaxURL := fmt.Sprintf("%s/%s/%s?vrf=%s", ajaxReadBase, chapterType, chapterID, url.QueryEscape(vrfToken))

	return c.fetchPagesJSON(ajaxURL)
}

// fetchPagesJSON re-fetches the AJAX URL as raw JSON and extracts
// the image URLs.
func (c *Client) fetchPagesJSON(ajaxURL string) ([]PageImage, error) {
	body, err := c.FetchJSON(ajaxURL)
	if err != nil {
		return nil, err
	}

	var pr chapterPagesResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("decode pages json: %w", err)
	}

	images := make([]PageImage, 0, len(pr.Result.Images))
	for _, img := range pr.Result.Images {
		if len(img) < 1 {
			continue
		}
		imgURL, ok := img[0].(string)
		if !ok || imgURL == "" {
			continue
		}
		// Extract scrambling offset (third element in the array).
		// When > 0, the image pixels are rearranged and need unscrambling.
		var offset int
		if len(img) >= 3 {
			if f, ok := img[2].(float64); ok {
				offset = int(f)
			}
		}

		images = append(images, PageImage{
			URL:             ResolveURL(imgURL),
			ScrambledOffset: offset,
		})
	}

	return images, nil
}

// ---------------------------------------------------------------------------
// Chapter list via AJAX
// ---------------------------------------------------------------------------

// ajaxChapter represents a chapter returned by the AJAX endpoint.
type ajaxChapter struct {
	ID     string
	Number string
	Title  string
	Date   string
	Type   string // "chapter" or "volume"
	Href   string
}

// FetchAJAXChapterList fetches the chapter list for a manga via the AJAX
// endpoint. The manga ID is the trailing numeric part of the slug (e.g.,
// "1" from "one-piece.1").
func (c *Client) FetchAJAXChapterList(mangaID, mangaType, langCode string) ([]ajaxChapter, error) {
	if mangaType == "" {
		mangaType = defaultType
	}
	if langCode == "" {
		langCode = defaultLang
	}

	vrfInput := mangaID + "@" + mangaType + "@" + langCode
	vrfToken, err := VRF(vrfInput)
	if err != nil {
		return nil, fmt.Errorf("vrf(%q): %w", vrfInput, err)
	}

	ajaxURL := fmt.Sprintf("%s/%s/%s/%s?vrf=%s",
		ajaxReadBase, mangaID, mangaType, langCode, url.QueryEscape(vrfToken))

	body, err := c.FetchJSON(ajaxURL)
	if err != nil {
		return nil, fmt.Errorf("ajax chapter list: %w", err)
	}

	var clr chapterListResponse
	if err := json.Unmarshal(body, &clr); err != nil {
		return nil, fmt.Errorf("decode chapter list json: %w", err)
	}

	html := clr.Result.HTML
	if html == "" {
		return nil, nil // no chapters for this branch
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse chapter html: %w", err)
	}

	var chapters []ajaxChapter
	doc.Find("ul li a").Each(func(_ int, s *goquery.Selection) {
		ac := ajaxChapter{
			Type: mangaType,
		}
		ac.ID, _ = s.Attr("data-id")
		ac.Number = s.AttrOr("data-number", "")
		ac.Title = strings.TrimSpace(s.AttrOr("title", ""))
		if ac.Title == "" {
			ac.Title = strings.TrimSpace(s.Text())
		}
		ac.Href, _ = s.Attr("href")

		// Date from the second span if present
		spans := s.Find("span")
		if spans.Length() >= 2 {
			ac.Date = strings.TrimSpace(spans.Eq(1).Text())
		}

		if ac.ID != "" || ac.Number != "" {
			chapters = append(chapters, ac)
		}
	})

	return chapters, nil
}

// ---------------------------------------------------------------------------
// Batch chapter page fetcher (parallel, resume-safe)
// ---------------------------------------------------------------------------

// FetchAllChapterPages fetches page image URLs for all chapters of a single
// manga. It returns a ChapterPagesFile per chapter with image URLs.
//
// skipIfExists: if true, checks for existing chapter files and skips them.
func (c *Client) FetchAllChapterPages(
	mangaSlug string,
	chapters []ajaxChapter,
	workers int,
	ratePerSec int,
	skipIfExists func(chapterNum string) bool,
) ([]ChapterPagesFile, error) {

	if workers < 1 {
		workers = 1
	}
	if ratePerSec < 1 {
		ratePerSec = 2
	}

	// Build work queue.
	work := make(chan ajaxChapter, len(chapters))
	var skipped int64
	for _, ch := range chapters {
		if skipIfExists != nil && skipIfExists(ch.Number) {
			atomic.AddInt64(&skipped, 1)
			continue
		}
		work <- ch
	}
	close(work)

	totalWork := len(work)
	if totalWork == 0 {
		log.Printf("[%s] All %d chapter page files already exist; nothing to do.", mangaSlug, len(chapters))
		return nil, nil
	}

	log.Printf("[%s] Fetching pages for %d chapters (%d already done, %d workers, %d req/s)",
		mangaSlug, totalWork, skipped, workers, ratePerSec)

	limiter := NewSharedRateLimiter(ratePerSec)
	defer limiter.Close()

	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		all   []ChapterPagesFile
		errs  []error
		count int64
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for ch := range work {
				limiter.Acquire()
				pages, err := c.FetchChapterPages(ch.ID, ch.Type)
				if err != nil {
					errMsg := fmt.Errorf("worker %d [%s] chapter %s: %w",
						workerID, mangaSlug, ch.Number, err)
					log.Printf("Warning: %v", errMsg)
					mu.Lock()
					errs = append(errs, errMsg)
					mu.Unlock()
					continue
				}

				entry := ChapterPagesFile{
					Slug:    mangaSlug,
					Chapter: ch.Number,
					Title:   ch.Title,
					Pages:   pages,
				}

				mu.Lock()
				all = append(all, entry)
				mu.Unlock()

				n64 := atomic.AddInt64(&count, 1)
				log.Printf("[%s] chapter %s: %d pages (worker %d, %d/%d)",
					mangaSlug, ch.Number, len(pages), workerID, n64, totalWork)

				time.Sleep(100 * time.Millisecond) // small extra gap between chapters
			}
		}(w)
	}

	wg.Wait()

	log.Printf("[%s] Complete: %d chapters with pages, %d skipped, %d errors",
		mangaSlug, len(all), skipped, len(errs))

	if len(errs) > 0 {
		return all, fmt.Errorf("%d errors, last: %v", len(errs), errs[len(errs)-1])
	}
	return all, nil
}

// FetchChapterListAndPages is a convenience that chains chapter-list fetch
// and page-image fetch for a single manga slug.
func (c *Client) FetchChapterListAndPages(
	slug string,
	mangaType, langCode string,
	workers int,
	ratePerSec int,
	skipCheck func(chapterNum string) bool,
) ([]ChapterPagesFile, error) {

	mangaID := extractMangaID(slug)
	if mangaID == "" {
		return nil, fmt.Errorf("cannot extract manga ID from slug %q", slug)
	}

	chapters, err := c.FetchAJAXChapterList(mangaID, mangaType, langCode)
	if err != nil {
		return nil, fmt.Errorf("fetch chapter list for %s: %w", slug, err)
	}

	if len(chapters) == 0 {
		log.Printf("[%s] No chapters found (mangaID=%s, type=%s, lang=%s)",
			slug, mangaID, mangaType, langCode)
		return nil, nil
	}

	return c.FetchAllChapterPages(slug, chapters, workers, ratePerSec, skipCheck)
}
