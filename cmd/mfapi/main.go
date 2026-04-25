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
	mode := flag.String("mode", "full", "Operation mode: list, detail, full, search")
	searchQuery := flag.String("query", "", "Search keyword (use with --mode=search)")
	limit := flag.Int("limit", 0, "Max results (0 = all)")
	outputDir := flag.String("output", "output", "Output directory for JSON files")
	rateLimit := flag.Duration("rate", mfire.DefaultRateLimit, "Minimum delay between requests")
	maxRetries := flag.Int("retries", mfire.DefaultMaxRetries, "Max retry attempts on failure")
	flag.Parse()

	// Validate flags
	switch *mode {
	case "list", "detail", "full", "search":
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %q. Valid: list, detail, full, search\n", *mode)
		os.Exit(1)
	}

	if *mode == "search" && *searchQuery == "" {
		fmt.Fprintf(os.Stderr, "--query is required for search mode\n")
		os.Exit(1)
	}

	// Ensure output dir exists.
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
		runDetail(client, writer, *outputDir, progress)

	case "search":
		runSearch(client, writer, *searchQuery, *limit, progress)

	case "full":
		runFull(client, writer, *limit, progress)
	}
}

// runList fetches all manga and writes the listing.
func runList(client *mfire.Client, writer *output.Writer, limit int, progress chan<- string) {
	log.Println("Fetching all manga listing...")
	items, pages, err := client.FetchAllManga(progress)
	if err != nil {
		log.Fatalf("FetchAllManga failed: %v", err)
	}

	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	log.Printf("Collected %d manga from %d pages", len(items), pages)

	if err := writer.WriteMangaList(items); err != nil {
		log.Fatalf("Write manga list: %v", err)
	}
	log.Printf("Wrote manga list to %s", filepath.Join(writer.BaseDir, "manga.json"))

	if err := writer.WriteIndex(len(items)); err != nil {
		log.Fatalf("Write index: %v", err)
	}
	log.Printf("Wrote index to %s", filepath.Join(writer.BaseDir, "index.json"))
}

// runDetail reads the existing manga list and fetches details for each.
func runDetail(client *mfire.Client, writer *output.Writer, outputDir string, progress chan<- string) {
	mangaFile := filepath.Join(outputDir, "manga.json")
	items, err := readMangaList(mangaFile)
	if err != nil {
		log.Fatalf("Read manga list: %v", err)
	}

	log.Printf("Fetching details for %d manga...", len(items))

	slugs := make([]string, len(items))
	for i, item := range items {
		slugs[i] = item.Slug
	}

	details, err := client.FetchAllMangaDetails(slugs, progress)
	if err != nil {
		log.Fatalf("FetchAllMangaDetails failed: %v", err)
	}

	log.Printf("Fetched details for %d manga", len(details))

	for _, d := range details {
		if err := writer.WriteMangaDetail(d); err != nil {
			log.Printf("Warning: write detail for %s: %v", d.Slug, err)
		}
	}

	log.Printf("Wrote %d detail files to %s", len(details), filepath.Join(writer.BaseDir, "manga"))
}

// runSearch performs a keyword search and writes results.
func runSearch(client *mfire.Client, writer *output.Writer, query string, limit int, progress chan<- string) {
	log.Printf("Searching for %q...", query)

	items, err := client.SearchManga(query, limit)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	log.Printf("Found %d results", len(items))

	if err := writer.WriteMangaList(items); err != nil {
		log.Fatalf("Write search results: %v", err)
	}
	log.Printf("Wrote %d search results to manga.json", len(items))
}

// runFull runs list then detail.
func runFull(client *mfire.Client, writer *output.Writer, limit int, progress chan<- string) {
	start := time.Now()
	log.Println("=== Starting full scrape ===")

	runList(client, writer, limit, progress)

	log.Println("=== List phase complete, starting detail phase ===")

	runDetail(client, writer, writer.BaseDir, progress)

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
