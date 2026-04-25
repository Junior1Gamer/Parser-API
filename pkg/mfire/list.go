package mfire

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	filterURL = "https://mangafire.to/filter"
	// itemsPerPage is how many manga cards appear on a filter page.
	itemsPerPage = 30
)

// FetchAllManga lists every manga available on mangafire.to by walking every
// page of the /filter endpoint. It returns a slice of MangaListItem and the
// total number of pages scanned.
//
// progress is an optional channel; if non-nil a status message is sent after
// each page.
func (c *Client) FetchAllManga(progress chan<- string) ([]MangaListItem, int, error) {
	var all []MangaListItem

	// Fetch page 1 to determine the total number of pages.
	doc, err := c.FetchDocument(filterURL + "?page=1")
	if err != nil {
		return nil, 0, fmt.Errorf("fetch page 1: %w", err)
	}

	totalPages := extractTotalPages(doc)
	if totalPages == 0 {
		// Fallback: if we can't find pagination, just try page 1.
		totalPages = 1
	}

	log.Printf("Found %d pages of manga listings", totalPages)

	// Parse page 1.
	p1Items := parseMangaCards(doc, 1)
	all = append(all, p1Items...)

	// Scrape remaining pages.
	for page := 2; page <= totalPages; page++ {
		pageURL := filterURL + "?page=" + strconv.Itoa(page)
		pDoc, pErr := c.FetchDocument(pageURL)
		if pErr != nil {
			log.Printf("Warning: page %d failed: %v; skipping", page, pErr)
			continue
		}
		items := parseMangaCards(pDoc, page)
		all = append(all, items...)

		if progress != nil {
			progress <- fmt.Sprintf("page %d/%d (%d manga collected)", page, totalPages, len(all))
		}

		// Gentle delay between pages
		time.Sleep(200 * time.Millisecond)
	}

	return all, totalPages, nil
}

// extractTotalPages reads the pagination widget and returns the last page
// number. It looks for a link containing "last" or the last numbered page.
func extractTotalPages(doc *goquery.Document) int {
	// Common pagination patterns on mangafire:
	//   <ul class="pagination"> <li><a href="?page=N">N</a></li> ... </ul>
	//   or a simple <a href="...?page=N"> for the last page.
	lastVal := 0

	doc.Find(".pagination a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		// Try to extract a page number from the href
		if strings.Contains(href, "page=") {
			parsed, err := url.Parse(href)
			if err != nil {
				return
			}
			if p := parsed.Query().Get("page"); p != "" {
				if n, err := strconv.Atoi(p); err == nil && n > lastVal {
					lastVal = n
				}
			}
		}
	})

	// Also check text content of pagination links for the last page number.
	doc.Find(".pagination li a, .pagination li span").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if n, err := strconv.Atoi(text); err == nil && n > lastVal {
			lastVal = n
		}
	})

	if lastVal > 0 {
		return lastVal
	}

	// Fallback: look for a "last" link or arrow link
	doc.Find(".pagination a[rel=last], .pagination a:contains('»')").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		if strings.Contains(href, "page=") {
			parsed, err := url.Parse(href)
			if err != nil {
				return
			}
			if p := parsed.Query().Get("page"); p != "" {
				if n, err := strconv.Atoi(p); err == nil && n > lastVal {
					lastVal = n
				}
			}
		}
	})

	return lastVal
}

// parseMangaCards extracts MangaListItems from a filter (or similar listing)
// page.
func parseMangaCards(doc *goquery.Document, page int) []MangaListItem {
	var items []MangaListItem

	// The listing typically has div.unit or similar card containers.
	// Selector strategy: find anchor tags that link to /manga/ and have
	// poster images.
	doc.Find("a[href*='/manga/']").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		// Normalise the URL.
		absoluteURL := ResolveURL(href)

		slug := ExtractSlug(absoluteURL)
		if slug == "" {
			return
		}

		// Extract title — prefer title attribute, then img alt, then text.
		title := strings.TrimSpace(s.AttrOr("title", ""))
		if title == "" {
			title = strings.TrimSpace(s.Text())
		}
		if title == "" {
			// Try the img alt
			title = strings.TrimSpace(s.Find("img").AttrOr("alt", ""))
		}
		if title == "" {
			return // skip entries with no title
		}

		// Get cover image from child img.
		cover, _ := s.Find("img").Attr("src")

		items = append(items, MangaListItem{
			Slug:  slug,
			Title: title,
			Cover: cover,
			URL:   absoluteURL,
		})
	})

	return items
}

// SearchManga performs a keyword search on mangafire.to and returns up to
// limit results.
func (c *Client) SearchManga(keyword string, limit int) ([]MangaListItem, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("empty keyword")
	}

	// Preflight: fetch the filter page to set cookies / session state.
	_, _ = c.FetchDocument(filterURL)

	// Build the search URL with VRF token.
	parts := strings.Fields(keyword)
	encodedParts := make([]string, len(parts))
	for i, p := range parts {
		encodedParts[i] = url.QueryEscape(p)
	}
	encodedQuery := strings.Join(encodedParts, "+")

	searchURL := filterURL + "?keyword=" + encodedQuery
	doc, err := c.FetchDocumentWithVRF(searchURL, keyword)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}

	items := parseMangaCards(doc, 0)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
