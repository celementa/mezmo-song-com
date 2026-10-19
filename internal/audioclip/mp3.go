// Нарезка mp3 по границам фреймов — 15-секундный отрывок песни для превью до
// оплаты (шаг 7 варианта 1).
//
// Почему свой код, а не ffmpeg: образ сервера — distroless/static, в нём нет ни
// шелла, ни библиотек, поэтому ffmpeg туда не поставить, а менять базовый образ
// ради одной нарезки — трогать деплой в самом чувствительном месте. Формат
// известен ровно один (Kie.ai отдаёт mp3), перекодировать нечего: отрывок
// собирается копированием готовых фреймов, без декодирования звука.
//
// Почему нельзя просто отдать байтовый префикс: длительность префикса зависит
// от битрейта, а у VBR-файла она гуляет в разы. Обход фреймов даёт точную
// длительность и для CBR, и для VBR — каждый фрейм несёт своё число сэмплов.
package audioclip

import (
	"errors"
	"time"
)

// ErrNoFrames — в данных не нашлось ни одного mp3-фрейма (не mp3, битый файл
// или обрезанная загрузка).
var ErrNoFrames = errors.New("audioclip: mp3 frames not found")

// Битрейты Layer III, кбит/с. Индекс 0 (free) и 15 (bad) не используются.
var bitratesMPEG1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
var bitratesMPEG2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}

// Частоты дискретизации по версии MPEG.
var sampleRates = map[byte][3]int{
	3: {44100, 48000, 32000}, // MPEG 1
	2: {22050, 24000, 16000}, // MPEG 2
	0: {11025, 12000, 8000},  // MPEG 2.5
}

type frame struct {
	offset  int
	length  int
	samples int
	rate    int
}

// Cut возвращает отрывок длиной d, начинающийся примерно на startFraction от
// начала записи (0 — с начала). Отрывок всегда начинается с целого фрейма,
// поэтому плеер его открывает как обычный mp3.
//
// Если после точки старта данных меньше, чем d, отрывок берётся с начала: это
// штатный случай для короткого трека, а не ошибка.
func Cut(src []byte, startFraction float64, d time.Duration) ([]byte, error) {
	frames := parseFrames(src)
	if len(frames) == 0 {
		return nil, ErrNoFrames
	}
	start := frameAtFraction(frames, startFraction)
	clip := collect(frames, start, d)
	if clip.duration < d && start > 0 {
		clip = collect(frames, 0, d)
	}
	if clip.length == 0 {
		return nil, ErrNoFrames
	}
	return src[clip.from : clip.from+clip.length], nil
}

// parseFrames обходит файл по цепочке фреймов. Мусор перед первым фреймом
// (ID3-тег, «мягкий» мусор от CDN) пропускается поиском синхрослова; после
// первого фрейма следующий ищется по длине текущего, а если там синхрослова
// нет — снова поиском, чтобы одиночный битый фрейм не обрывал разбор.
func parseFrames(src []byte) []frame {
	frames := make([]frame, 0, 1024)
	pos := 0
	for pos+4 <= len(src) {
		f, ok := parseHeader(src, pos)
		if !ok {
			next := findSync(src, pos+1)
			if next < 0 {
				break
			}
			pos = next
			continue
		}
		if f.offset+f.length > len(src) {
			break // хвост обрезан — неполный фрейм в отрывок не берём
		}
		frames = append(frames, f)
		pos = f.offset + f.length
	}
	return frames
}

func findSync(src []byte, from int) int {
	for i := from; i+4 <= len(src); i++ {
		if src[i] == 0xFF && src[i+1]&0xE0 == 0xE0 {
			if _, ok := parseHeader(src, i); ok {
				return i
			}
		}
	}
	return -1
}

// parseHeader разбирает заголовок фрейма Layer III по смещению pos.
func parseHeader(src []byte, pos int) (frame, bool) {
	if pos+4 > len(src) {
		return frame{}, false
	}
	b0, b1, b2 := src[pos], src[pos+1], src[pos+2]
	if b0 != 0xFF || b1&0xE0 != 0xE0 {
		return frame{}, false
	}
	version := (b1 >> 3) & 0x03
	if version == 1 { // зарезервировано
		return frame{}, false
	}
	layer := (b1 >> 1) & 0x03
	if layer != 1 { // 01 = Layer III, остальные нам не встречаются
		return frame{}, false
	}
	bitrateIdx := (b2 >> 4) & 0x0F
	rateIdx := (b2 >> 2) & 0x03
	if rateIdx == 3 {
		return frame{}, false
	}
	padding := int((b2 >> 1) & 0x01)

	rate := sampleRates[version][rateIdx]
	if rate == 0 {
		return frame{}, false
	}
	var bitrate, samples, coef int
	if version == 3 { // MPEG 1
		bitrate, samples, coef = bitratesMPEG1[bitrateIdx]*1000, 1152, 144
	} else { // MPEG 2 / 2.5
		bitrate, samples, coef = bitratesMPEG2[bitrateIdx]*1000, 576, 72
	}
	if bitrate == 0 {
		return frame{}, false
	}
	length := coef*bitrate/rate + padding
	if length <= 4 {
		return frame{}, false
	}
	return frame{offset: pos, length: length, samples: samples, rate: rate}, true
}

// frameAtFraction — индекс фрейма, с которого начинается отрывок. Считается по
// длительности, а не по номеру фрейма: у VBR фреймы разной длины.
func frameAtFraction(frames []frame, fraction float64) int {
	if fraction <= 0 {
		return 0
	}
	total := time.Duration(0)
	for _, f := range frames {
		total += frameDuration(f)
	}
	target := time.Duration(float64(total) * fraction)
	acc := time.Duration(0)
	for i, f := range frames {
		if acc >= target {
			return i
		}
		acc += frameDuration(f)
	}
	return 0
}

type clipRange struct {
	from     int
	length   int
	duration time.Duration
}

// collect набирает фреймы, начиная с start, пока не наберётся d.
func collect(frames []frame, start int, d time.Duration) clipRange {
	if start >= len(frames) {
		return clipRange{}
	}
	out := clipRange{from: frames[start].offset}
	for i := start; i < len(frames); i++ {
		out.length += frames[i].length
		out.duration += frameDuration(frames[i])
		if out.duration >= d {
			break
		}
	}
	return out
}

func frameDuration(f frame) time.Duration {
	return time.Duration(f.samples) * time.Second / time.Duration(f.rate)
}
