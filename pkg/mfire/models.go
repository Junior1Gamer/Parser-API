package mfire

// MangaListItem represents a single entry in the all-manga listing.
type MangaListItem struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Cover string `json:"cover,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MangaID extracts the numeric manga ID from the slug.
// A slug like "one-piece.1" returns "1".
func (m MangaListItem) MangaID() string {
	return extractMangaID(m.Slug)
}

// MangaDetail is the full metadata for a single manga, including its chapters.
type MangaDetail struct {
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Cover       string    `json:"cover,omitempty"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	AltTitles   []string  `json:"alt_titles,omitempty"`
	Chapters    []Chapter `json:"chapters,omitempty"`
	UpdatedAt   string    `json:"updated_at,omitempty"`
	FetchedAt   string    `json:"fetched_at,omitempty"` // when we last fetched this detail
}

// Chapter represents a single chapter of a manga.
type Chapter struct {
	Number string `json:"number"`
	Title  string `json:"title,omitempty"`
	Date   string `json:"date,omitempty"`
	URL    string `json:"url,omitempty"`
}

// PageImage holds a single page image URL and its scrambling offset.
// When ScrambledOffset > 0, the image pixels are rearranged in a grid pattern
// and must be unscrambled by the consumer using the offset as the seed.
type PageImage struct {
	URL             string `json:"url"`
	ScrambledOffset int    `json:"scrambled_offset,omitempty"`
}

// IsScrambled returns true when the page image needs unscrambling.
func (p PageImage) IsScrambled() bool { return p.ScrambledOffset > 0 }

// ChapterPagesFile holds the page image URLs for a single chapter.
type ChapterPagesFile struct {
	Slug    string      `json:"slug"`
	Chapter string      `json:"chapter"`
	Title   string      `json:"title,omitempty"`
	Pages   []PageImage `json:"pages"`
}

// Metadata is the top-level metadata for the output dataset.
type Metadata struct {
	GeneratedAt   string `json:"generated_at"`
	TotalManga    int    `json:"total_manga"`
	MangaFile     string `json:"manga_file"`
	DetailPrefix  string `json:"detail_prefix"`
	ChaptersDir   string `json:"chapters_dir,omitempty"`
}

// AJAX responses from mangafire.to

// chapterListResponse is the JSON response from /ajax/read/{mangaId}/{type}/{langCode}
type chapterListResponse struct {
	Result struct {
		HTML string `json:"html"`
	} `json:"result"`
}

// chapterPagesResponse is the JSON response from /ajax/read/{type}/{chapterId}
type chapterPagesResponse struct {
	Result struct {
		Images [][]interface{} `json:"images"` // each entry: [url, preview_url, offset]
	} `json:"result"`
}

// FetchIndexEntry tracks when a manga was last fetched and its status.
type FetchIndexEntry struct {
	FetchedAt string `json:"fetched_at"`
	Status    string `json:"status,omitempty"`
}

// FetchIndex is a lightweight per-manga fetch tracker stored on the output
// branch as fetch_index.json.  It allows the refresh logic to decide which
// slugs need re-fetching without reading every detail JSON file.
type FetchIndex struct {
	Entries map[string]FetchIndexEntry `json:"entries"`
}

// extractMangaID gets the trailing numeric ID from a slug like "one-piece.1".
func extractMangaID(slug string) string {
	idx := len(slug) - 1
	for idx >= 0 && slug[idx] != '.' {
		idx--
	}
	if idx < 0 {
		return slug
	}
	return slug[idx+1:]
}
