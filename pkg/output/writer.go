package output

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Junior1Gamer/MF-API/pkg/mfire"
)

// Writer handles writing structured JSON output files to a directory.
type Writer struct {
	BaseDir string
}

// NewWriter creates a Writer that writes to the given base directory.
func NewWriter(baseDir string) *Writer {
	return &Writer{BaseDir: baseDir}
}

// WriteMetadata writes the top-level metadata file (metadata.json).
func (w *Writer) WriteMetadata(total int) error {
	meta := mfire.Metadata{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		TotalManga:   total,
		MangaFile:    "manga.json",
		DetailPrefix: "manga/",
	}
	return w.writeJSON("metadata.json", meta)
}

// WriteMangaList writes the full manga listing as an array.
func (w *Writer) WriteMangaList(items []mfire.MangaListItem) error {
	return w.writeJSON("manga.json", items)
}

// WriteMangaDetail writes a single manga's detail to manga/{slug}.json.
func (w *Writer) WriteMangaDetail(detail mfire.MangaDetail) error {
	dir := filepath.Join("manga")
	path := filepath.Join(dir, detail.Slug+".json")
	return w.writeJSON(path, detail)
}

// writeJSON marshals v as indented JSON and writes it to relPath under BaseDir.
func (w *Writer) writeJSON(relPath string, v interface{}) error {
	fullPath := filepath.Join(w.BaseDir, relPath)

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	return mfire.WriteFileAtomic(fullPath, data, 0644)
}
