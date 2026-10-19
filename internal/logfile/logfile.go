// Package logfile — лог приложения в файл с ротацией по дням.
//
// `docker logs` живут только до пересоздания контейнера: каждый деплой
// (`docker compose up -d --build app`) начинает историю с нуля, и разбор
// вчерашнего вебхука ЮKassa становится невозможен. Файлы на томе данных
// переживают деплой; старше keepDays — удаляются при первой записи нового дня.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const filePrefix = "app-"

// Writer пишет в <dir>/app-YYYY-MM-DD.log, переключая файл при смене даты (UTC).
// Ошибка записи в файл не роняет приложение: stdout остаётся основным выводом.
type Writer struct {
	dir      string
	keepDays int

	mu  sync.Mutex
	day string
	f   *os.File
}

func New(dir string, keepDays int) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if keepDays < 1 {
		keepDays = 1
	}
	return &Writer{dir: dir, keepDays: keepDays}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	day := time.Now().UTC().Format("2006-01-02")
	if w.f == nil || day != w.day {
		if err := w.rotate(day); err != nil {
			return len(p), nil // stdout уже получил строку; файл — вспомогательный
		}
	}
	_, _ = w.f.Write(p)
	return len(p), nil
}

func (w *Writer) rotate(day string) error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	f, err := os.OpenFile(filepath.Join(w.dir, filePrefix+day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w.f, w.day = f, day
	w.prune()
	return nil
}

// prune удаляет файлы старше keepDays. Имена сортируются лексикографически —
// для ISO-дат это и есть хронология.
func (w *Writer) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, filePrefix) && strings.HasSuffix(n, ".log") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for len(names) > w.keepDays {
		_ = os.Remove(filepath.Join(w.dir, names[0]))
		names = names[1:]
	}
}

// Path — каталог логов (для стартовой строки в stdout).
func (w *Writer) Path() string { return w.dir }

// Current — имя файла за сегодня (для тестов и подсказок оператору).
func (w *Writer) Current() string {
	return fmt.Sprintf("%s%s.log", filePrefix, time.Now().UTC().Format("2006-01-02"))
}
