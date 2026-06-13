package stats

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// FileLogger appends JSON-encoded entries to a file, one per line,
// keeping only the most recent maxLines lines.
type FileLogger struct {
	mu       sync.Mutex
	path     string
	maxLines int
}

// NewFileLogger returns a FileLogger that writes to path, trimming it to
// the most recent maxLines lines after each append.
func NewFileLogger(path string, maxLines int) *FileLogger {
	return &FileLogger{path: path, maxLines: maxLines}
}

// Append marshals entry as JSON and writes it as the last line of the
// file, dropping the oldest lines if the file exceeds maxLines.
func (f *FileLogger) Append(entry any) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var lines []string
	if data, err := os.ReadFile(f.path); err == nil {
		if trimmed := strings.TrimRight(string(data), "\n"); trimmed != "" {
			lines = strings.Split(trimmed, "\n")
		}
	}
	lines = append(lines, string(line))
	if len(lines) > f.maxLines {
		lines = lines[len(lines)-f.maxLines:]
	}
	return os.WriteFile(f.path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
