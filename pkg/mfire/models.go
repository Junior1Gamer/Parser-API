package mfire

// MangaListItem represents a single entry in the all-manga listing.
type MangaListItem struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Cover string `json:"cover,omitempty"`
	URL   string `json:"url,omitempty"`
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
}

// Chapter represents a single chapter of a manga.
type Chapter struct {
	Number string `json:"number"`
	Title  string `json:"title,omitempty"`
	Date   string `json:"date,omitempty"`
	URL    string `json:"url,omitempty"`
}

// IndexMeta is the top-level metadata for the output dataset.
type IndexMeta struct {
	GeneratedAt  string `json:"generated_at"`
	TotalManga   int    `json:"total_manga"`
	MangaFile    string `json:"manga_file"`
	DetailPrefix string `json:"detail_prefix"`
}
