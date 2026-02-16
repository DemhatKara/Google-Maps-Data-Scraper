package webrunner

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"sync"

	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/scrapemate"
)

// RotatingCsvWriter implements scrapemate.ResultWriter and rotates files every Limit records.
type RotatingCsvWriter struct {
	mu           sync.Mutex
	baseFileName string
	limit        int
	currentCount int
	fileIndex    int

	currentFile *os.File
	csvWriter   *csv.Writer

	OnWrite func(int)
}

// NewRotatingCsvWriter creates a new rotating writer.
func NewRotatingCsvWriter(baseFileName string, limit int) *RotatingCsvWriter {
	return &RotatingCsvWriter{
		baseFileName: baseFileName,
		limit:        limit,
		fileIndex:    1,
	}
}

// Run consumes the results channel and manages file rotation
func (w *RotatingCsvWriter) Run(ctx context.Context, in <-chan scrapemate.Result) error {
	defer func() {
		w.mu.Lock()
		if w.currentFile != nil {
			w.csvWriter.Flush()
			w.currentFile.Close()
		}
		w.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res, ok := <-in:
			if !ok {
				return nil
			}
			// We pass the result directly to Write, but we need to ensure Write handles it
			if err := w.Write(ctx, res); err != nil {
				return err
			}
		}
	}
}

// Write writes a single record, rotating the file if needed.
func (w *RotatingCsvWriter) Write(ctx context.Context, data any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Unwrap scrapemate.Result if needed
	payload := data
	if res, ok := data.(scrapemate.Result); ok {
		payload = res.Data
	}

	// Handle slice of entries (which is what we get from scrapemate)
	if entries, ok := payload.([]*gmaps.Entry); ok {
		for _, entry := range entries {
			if err := w.writeOne(ctx, entry); err != nil {
				return err
			}
		}
		return nil
	}

	// Fallback for single entry or other types
	return w.writeOne(ctx, payload)
}

func (w *RotatingCsvWriter) writeOne(ctx context.Context, payload any) error {
	// Initialize first file if not open
	if w.currentFile == nil {
		if err := w.rotate(payload); err != nil {
			return err
		}
	}

	// Check rotation limit
	if w.currentCount >= w.limit {
		if err := w.rotate(payload); err != nil {
			return err
		}
	}

	// Prepare record
	var record []string

	if entry, ok := payload.(gmaps.Entry); ok {
		record = entry.CsvRow()
	} else if entryPtr, ok := payload.(*gmaps.Entry); ok {
		record = entryPtr.CsvRow()
	} else if row, ok := payload.([]string); ok { // Support raw strings/manual test
		record = row
	} else {
		return fmt.Errorf("invalid data type for csv writer: %T (payload: %T)", payload, payload)
	}

	if err := w.csvWriter.Write(record); err != nil {
		return err
	}

	w.currentCount++

	// Flush periodically for data safety (every 100 rows instead of every row)
	if w.currentCount%100 == 0 {
		w.csvWriter.Flush()
	}

	if w.OnWrite != nil {
		w.OnWrite(1)
	}

	return nil
}

func (w *RotatingCsvWriter) rotate(sampleData any) error {
	// Close existing file
	if w.currentFile != nil {
		w.csvWriter.Flush()
		w.currentFile.Close()
	}

	filename := fmt.Sprintf("%s_%d.csv", w.baseFileName, w.fileIndex)
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create rotating file %s: %w", filename, err)
	}

	// Write BOM
	f.Write([]byte{0xEF, 0xBB, 0xBF})

	w.currentFile = f
	w.csvWriter = csv.NewWriter(f)
	w.currentCount = 0
	w.fileIndex++

	// Write Headers if available
	var headers []string

	// Helper to get headers from a single item
	getHeaders := func(item any) []string {
		if entry, ok := item.(gmaps.Entry); ok {
			return entry.CsvHeaders()
		}
		if entryPtr, ok := item.(*gmaps.Entry); ok {
			return entryPtr.CsvHeaders()
		}
		return nil
	}

	if entries, ok := sampleData.([]*gmaps.Entry); ok && len(entries) > 0 {
		headers = getHeaders(entries[0])
	} else {
		headers = getHeaders(sampleData)
	}

	if len(headers) > 0 {
		if err := w.csvWriter.Write(headers); err != nil {
			return err
		}
		w.csvWriter.Flush()
	}

	return nil
}

// Close closes the current file.
func (w *RotatingCsvWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.currentFile != nil {
		w.csvWriter.Flush()
		err := w.currentFile.Close()
		w.currentFile = nil
		w.csvWriter = nil
		return err
	}
	return nil
}
