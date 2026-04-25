package mfire

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const mangaBaseURL = "https://mangafire.to/manga/"

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
	// Try h1[itemprop="name"] first, then og:title, then h1.
	result.Title = strings.TrimSpace(doc.Find("h1[itemprop=name]").First().Text())
	if result.Title == "" {
		doc.Find("meta[property='og:title']").Each(func(_ int, s *goquery.Selection) {
			if c, ok := s.Attr("content"); ok {
				result.Title = strings.TrimSpace(c)
			}
		})
	}
	if result.Title == "" {
		result.Title = strings.TrimSpace(doc.Find("h1").First().Text())
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
			// Skip generic descriptions
			if len(desc) > 50 && !strings.Contains(desc, "read manga") {
				result.Description = desc
			}
		}
	})
	// Fallback: div.description or div.synopsis
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
		// Alternative: look in meta tags or lists
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
		// Remove label prefix
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
	chapters := parseChapters(doc)
	result.Chapters = chapters

	return result, nil
}

// parseChapters extracts the chapter list from a manga detail page.
func parseChapters(doc *goquery.Document) []Chapter {
	var chapters []Chapter

	// The chapter list is typically a <ul> with <li> containing <a> elements.
	// Each list item may have a data-number attribute for the chapter number.
	doc.Find("ul li[data-number], .list-body li, .chapters li, .chapter-list li").Each(func(_ int, s *goquery.Selection) {
		ch := Chapter{}

		// Try data-number first.
		if numStr, ok := s.Attr("data-number"); ok {
			ch.Number = strings.TrimSpace(numStr)
		}

		// Chapter link.
		link := s.Find("a").First()
		ch.URL, _ = link.Attr("href")
		ch.URL = ResolveURL(ch.URL)

		// Chapter title from link text or title attribute.
		ch.Title = strings.TrimSpace(link.AttrOr("title", ""))
		if ch.Title == "" {
			ch.Title = strings.TrimSpace(link.Text())
		}

		// If we didn't get a number from data-number, extract from the link
		// href or text.
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

		// Date.
		ch.Date = strings.TrimSpace(s.Find("span.date, .chapter-date, time").First().Text())

		if ch.Number != "" || ch.Title != "" {
			chapters = append(chapters, ch)
		}
	})

	// Fallback: simple chapter links.
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

// isChapterNumber reports whether s looks like a chapter number, e.g. "1",
// "10.5", "ch1", "chapter-1".
func isChapterNumber(s string) bool {
	s = strings.TrimPrefix(s, "ch")
	s = strings.TrimPrefix(s, "chapter-")
	s = strings.TrimPrefix(s, "chapter.")
	s = strings.TrimPrefix(s, "v")
	// Now it should be a number like "1", "10.5" etc.
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

// FetchAllMangaDetails sequentially fetches details for all slugs in the list.
// It sends progress messages on the channel when non-nil.
func (c *Client) FetchAllMangaDetails(slugs []string, progress chan<- string) ([]MangaDetail, error) {
	var details []MangaDetail

	for i, slug := range slugs {
		detail, err := c.FetchMangaDetail(slug)
		if err != nil {
			log.Printf("Warning: detail fetch failed for %s: %v; skipping", slug, err)
			continue
		}
		details = append(details, *detail)

		if progress != nil {
			progress <- fmt.Sprintf("detail %d/%d: %s", i+1, len(slugs), slug)
		}

		// Be nice to the server.
		time.Sleep(300 * time.Millisecond)
	}

	return details, nil
}
