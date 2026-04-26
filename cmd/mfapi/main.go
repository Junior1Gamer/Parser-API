package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Junior1Gamer/MF-API/pkg/mfire"
	"github.com/Junior1Gamer/MF-API/pkg/output"
)

func main() {
	mode := flag.String("mode", "full", "Operation mode: list, detail, full, search, chapters")
	searchQuery := flag.String("query", "", "Search keyword (use with --mode=search)")
	limit := flag.Int("limit", 0, "Max results (0 = all)")
	outputDir := flag.String("output", "output", "Output directory for JSON files")
	rateLimit := flag.Duration("rate", mfire.DefaultRateLimit, "Minimum delay between requests (serial only)")
	maxRetries := flag.Int("retries", mfire.DefaultMaxRetries, "Max retry attempts on failure")
	parallel := flag.Int("parallel", 4, "Number of concurrent detail/chapter workers (0 = serial)")
	ratePerSec := flag.Int("rate-per-sec", 3, "Global rate limit in req/s (used with --parallel)")
	mangaSlug := flag.String("slug", "", "Single manga slug (use with --mode=chapters)")
	chapterType := flag.String("chapter-type", "chapter", "Branch type: chapter or volume")
	chapterLang := flag.String("chapter-lang", "en", "Chapter language code")
	flag.Parse()

	switch *mode {
	case "list", "detail", "full", "search", "chapters":
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %q. Valid: list, detail, full, search, chapters\n", *mode)
		os.Exit(1)
	}
	if *mode == "search" && *searchQuery == "" {
		fmt.Fprintf(os.Stderr, "--query is required for search mode\n")
		os.Exit(1)
	}
	if *parallel < 0 {
		*parallel = 1
	}

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Create output dir: %v", err)
	}

	client := mfire.NewClient()
	client.SetRateLimit(*rateLimit)
	client.SetMaxRetries(*maxRetries)

	writer := output.NewWriter(*outputDir)

	progress := make(chan string, 100)
	go func() {
		for msg := range progress {
			log.Println(msg)
		}
	}()

	switch *mode {
	case "list":
		runList(client, writer, *limit, progress)
	case "detail":
		runDetail(client, *parallel, *ratePerSec, progress)
	case "search":
		runSearch(client, writer, *searchQuery, *limit, progress)
	case "chapters":
		runChapters(client, *outputDir, *mangaSlug, *chapterType, *chapterLang, *parallel, *ratePerSec, *limit, progress)
	case "full":
		runFull(client, writer, *limit, *parallel, *ratePerSec, progress)
	}
}

// runList fetches all manga and writes the listing.
func runList(client *mfire.Client, w *output.Writer, limit int, progress chan<- string) {
	log.Println("Fetching all manga listing...")
	items, pages, err := client.FetchAllManga(progress)
	if err != nil {
		log.Fatalf("FetchAllManga failed: %v", err)
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	log.Printf("Collected %d manga from %d pages", len(items), pages)

	if err := w.WriteMangaList(items); err != nil {
		log.Fatalf("Write manga list: %v", err)
	}
	if err := w.WriteIndex(len(items)); err != nil {
		log.Fatalf("Write index: %v", err)
	}
	log.Printf("Wrote listing (%d entries) and index", len(items))
}

// runDetail reads the manga list and fetches details, supporting resume.
func runDetail(client *mfire.Client, parallel, ratePerSec int, progress chan<- string) {
	if err := os.MkdirAll("output/manga", 0755); err != nil {
		log.Fatalf("Create manga dir: %v", err)
	}

	items, err := readMangaList("output/manga.json")
	if err != nil {
		log.Fatalf("Read manga list: %v", err)
	}

	slugs := make([]string, len(items))
	for i, item := range items {
		slugs[i] = item.Slug
	}

	if parallel > 0 {
		detailWriter := mfire.NewDirectDetailWriter("output")
		fetched, err := client.FetchAllMangaDetailsParallel(slugs, parallel, ratePerSec, detailWriter, progress)
		if err != nil {
			log.Fatalf("Parallel detail fetch failed: %v", err)
		}
		log.Printf("Fetched %d new manga details (parallel=%d, rate=%d/s)", fetched, parallel, ratePerSec)
	} else {
		details, err := client.FetchAllMangaDetails(slugs, progress)
		if err != nil {
			log.Fatalf("Serial detail fetch failed: %v", err)
		}
		for _, d := range details {
			if we := writeDetailFile("output", d); we != nil {
				log.Printf("Warning: write %s: %v", d.Slug, we)
			}
		}
		log.Printf("Fetched %d manga details (serial)", len(details))
	}
}

// runSearch performs a keyword search.
func runSearch(client *mfire.Client, w *output.Writer, query string, limit int, progress chan<- string) {
	log.Printf("Searching for %q...", query)
	items, err := client.SearchManga(query, limit)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}
	log.Printf("Found %d results", len(items))
	if err := w.WriteMangaList(items); err != nil {
		log.Fatalf("Write search results: %v", err)
	}
}

// runChapters fetches page image URLs for one or all manga's chapters.
// If --slug is set, only that manga is processed; otherwise all manga from
// the manga.json listing are iterated.
func runChapters(client *mfire.Client, outputDir, slug, chapType, chapLang string, parallel, ratePerSec, limit int, progress chan<- string) {
	if slug != "" {
		runChapterForSlug(client, slug, chapType, chapLang, parallel, ratePerSec, progress)
		return
	}

	items, err := readMangaList(filepath.Join(outputDir, "manga.json"))
	if err != nil {
		log.Fatalf("Read manga list: %v", err)
	}

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	log.Printf("Fetching chapter pages for %d manga...", len(items))

	start := time.Now()
	var totalManga, totalChapters, totalSkipped int

	for i, item := range items {
		mangaStart := time.Now()
		skipped := writeChapterPagesForSlug(client, outputDir, item.Slug, chapType, chapLang, parallel, ratePerSec, progress)
		elapsed := time.Since(mangaStart).Round(time.Second)

		totalManga++
		totalChapters += skipped.total
		totalSkipped += skipped.skipped

		log.Printf("[%d/%d] %s: %d chapter files (%d skipped, took %s)",
			i+1, len(items), item.Slug, skipped.total, skipped.skipped, elapsed)

		if progress != nil {
			progress <- fmt.Sprintf("manga %d/%d: %s", i+1, len(items), item.Slug)
		}
	}

	log.Printf("=== Chapter pages complete: %d manga, %d total chapter files, %d skipped, took %s ===",
		totalManga, totalChapters, totalSkipped, time.Since(start).Round(time.Second))
}

// runChapterForSlug processes a single manga slug.
func runChapterForSlug(client *mfire.Client, slug, chapType, chapLang string, parallel, ratePerSec int, progress chan<- string) {
	log.Printf("Fetching chapter pages for %s...", slug)
	start := time.Now()
	skipped := writeChapterPagesForSlug(client, "output", slug, chapType, chapLang, parallel, ratePerSec, progress)
	log.Printf("%s: %d chapter files (%d skipped, took %s)",
		slug, skipped.total, skipped.skipped, time.Since(start).Round(time.Second))
}

// chapterStats tracks counts from a chapter pages run.
type chapterStats struct {
	total   int
	skipped int
}

// writeChapterPagesForSlug fetches chapter pages and writes them.
func writeChapterPagesForSlug(client *mfire.Client, outputDir, slug, chapType, chapLang string, parallel, ratePerSec int, progress chan<- string) chapterStats {
	skipCheck := func(chapterNum string) bool {
		p := filepath.Join(outputDir, "manga", slug, "chapters", chapterNum+".json")
		_, err := os.Stat(p)
		return err == nil
	}

	results, err := client.FetchChapterListAndPages(slug, chapType, chapLang, parallel, ratePerSec, skipCheck)
	if err != nil {
		log.Printf("Warning: %s chapter pages: %v", slug, err)
	}
	if results == nil {
		return chapterStats{}
	}

	written := 0
	for _, cp := range results {
		dir := filepath.Join(outputDir, "manga", slug, "chapters")
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: mkdir %s: %v", dir, err)
			continue
		}
		data, err := json.MarshalIndent(cp, "", "  ")
		if err != nil {
			log.Printf("Warning: marshal %s ch %s: %v", slug, cp.Chapter, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, cp.Chapter+".json"), data, 0644); err != nil {
			log.Printf("Warning: write %s ch %s: %v", slug, cp.Chapter, err)
			continue
		}
		written++
	}

	return chapterStats{total: written}
}

// runFull runs list then detail.
func runFull(client *mfire.Client, w *output.Writer, limit, parallel, ratePerSec int, progress chan<- string) {
	start := time.Now()
	log.Println("=== Starting full scrape ===")

	runList(client, w, limit, progress)
	log.Println("=== List phase complete, starting detail phase ===")
	runDetail(client, parallel, ratePerSec, progress)

	log.Printf("=== Full scrape complete in %s ===", time.Since(start).Round(time.Second))
}

// readMangaList reads the manga listing from a JSON file.
func readMangaList(path string) ([]mfire.MangaListItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var items []mfire.MangaListItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return items, nil
}

// writeDetailFile writes a single manga detail JSON to output/manga/{slug}.json.
func writeDetailFile(baseDir string, d mfire.MangaDetail) error {
	dir := filepath.Join(baseDir, "manga")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, d.Slug+".json"), data, 0644)
}
