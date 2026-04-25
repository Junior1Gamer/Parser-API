package output

import (
	"encoding/json"
	"fmt"
	"os"
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

// WriteIndex writes the top-level metadata file.
func (w *Writer) WriteIndex(total int) error {
	meta := mfire.IndexMeta{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		TotalManga:   total,
		MangaFile:    "manga.json",
		DetailPrefix: "manga/",
	}
	return w.writeJSON("index.json", meta)
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
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return fmt.Errorf("write file %s: %w", fullPath, err)
	}

	return nil
}
