package audioclip

import (
	"bytes"
	"testing"
	"time"
)

// mpeg1Frame собирает фрейм MPEG1 Layer III 128 кбит/с 44.1 кГц: 417 байт,
// 1152 сэмпла, ~26.12 мс.
func mpeg1Frame() []byte {
	f := make([]byte, 417)
	f[0], f[1] = 0xFF, 0xFB // sync + MPEG1 + Layer III + no CRC
	f[2] = 0x90             // bitrate 128, rate 44100, no padding
	f[3] = 0xC0
	return f
}

// mp3Of собирает запись из frames фреймов. Тело каждого помечено его номером —
// иначе отрывок из середины побайтово совпал бы с началом файла и проверка
// «взяли именно середину» не значила бы ничего.
func mp3Of(frames int) []byte {
	var buf bytes.Buffer
	for i := 0; i < frames; i++ {
		f := mpeg1Frame()
		f[4] = byte(i)
		buf.Write(f)
	}
	return buf.Bytes()
}

const frameDur = time.Duration(1152) * time.Second / 44100

func TestCutTakesRequestedDuration(t *testing.T) {
	// 60 секунд записи, отрывок 15 секунд с четверти файла.
	src := mp3Of(int(60 * time.Second / frameDur))
	clip, err := Cut(src, 0.25, 15*time.Second)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	got := time.Duration(len(clip)/417) * frameDur
	if got < 15*time.Second || got > 15*time.Second+frameDur {
		t.Fatalf("длительность отрывка %v, ожидалось ~15s", got)
	}
	if len(clip)%417 != 0 {
		t.Fatalf("отрывок оборван посреди фрейма: %d байт", len(clip))
	}
	if clip[0] != 0xFF || clip[1]&0xE0 != 0xE0 {
		t.Fatalf("отрывок начинается не с синхрослова: % x", clip[:4])
	}
	// Отрывок должен быть взят из середины, а не с начала файла.
	if bytes.Equal(clip, src[:len(clip)]) {
		t.Fatal("отрывок совпал с началом файла, хотя запрошена четверть")
	}
}

// Короткий трек: 15 секунд из него не набрать, и отрывок обязан начинаться с
// начала записи, а не отдавать пустоту с середины.
func TestCutShortTrackFallsBackToStart(t *testing.T) {
	src := mp3Of(int(5 * time.Second / frameDur))
	clip, err := Cut(src, 0.25, 15*time.Second)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if !bytes.Equal(clip, src) {
		t.Fatalf("ожидался весь трек (%d байт), получено %d", len(src), len(clip))
	}
}

// ID3-тег и мусор от CDN перед первым фреймом — обычное дело; разбор должен
// найти первый фрейм поиском синхрослова.
func TestCutSkipsLeadingGarbage(t *testing.T) {
	src := append([]byte("ID3\x04\x00\x00\x00\x00\x00\x21junkjunkjunk"), mp3Of(600)...)
	clip, err := Cut(src, 0, 15*time.Second)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if clip[0] != 0xFF {
		t.Fatalf("отрывок начался с мусора: % x", clip[:4])
	}
}

func TestCutRejectsNonMP3(t *testing.T) {
	if _, err := Cut(bytes.Repeat([]byte{0x00, 0x01, 0x02}, 500), 0.25, 15*time.Second); err != ErrNoFrames {
		t.Fatalf("ожидалась ErrNoFrames, получено %v", err)
	}
}

// Обрезанный на середине фрейма хвост в отрывок не попадает: плеер получил бы
// битый последний фрейм.
func TestCutDropsTruncatedTail(t *testing.T) {
	src := append(mp3Of(600), mpeg1Frame()[:100]...)
	clip, err := Cut(src, 0, 15*time.Second)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if len(clip)%417 != 0 {
		t.Fatalf("в отрывок попал неполный фрейм: %d байт", len(clip))
	}
}
