package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCreatesDailyFileAndPrunes(t *testing.T) {
	dir := t.TempDir()
	// Три «старых» дня уже лежат в каталоге.
	for _, d := range []string{"2020-01-01", "2020-01-02", "2020-01-03"} {
		if err := os.WriteFile(filepath.Join(dir, filePrefix+d+".log"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, w.Current()))
	if err != nil {
		t.Fatalf("today's file missing: %v", err)
	}
	if !strings.Contains(string(got), "hello") {
		t.Fatalf("today's file = %q, want hello", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("after prune keepDays=2: %d files %v, want 2 (today + newest old)", len(entries), names)
	}
}
