// Package storage — хранилище треков на локальном диске сервера: у
// mezmo-song.com один небольшой сервер, и отдельный MinIO ради пары тысяч mp3
// был бы лишним сервисом. Ключи остаются в формате объектного хранилища
// (orders/{id}/r{n}/track-{n}.mp3) — так их строят и читают вызывающие, — и
// превращаются в пути внутри Dir.
package storage

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ограничения загрузчика внешнего аудио. Трек Kie.ai — единицы мегабайт;
// лимит защищает 1 ГБ RAM сервера от ответа произвольного размера.
const MaxAudioBytes = 32 << 20 // 32 MiB

var (
	ErrBlockedAddress = errors.New("storage: destination address is not public")
	ErrHostNotAllowed = errors.New("storage: host is not in allowlist")
	ErrTooLarge       = errors.New("storage: response exceeds size limit")
	ErrBadKey         = errors.New("storage: key escapes storage dir")
)

// RetryDelay — пауза между попытками скачивания в DownloadAndStore. Переменная
// уровня пакета, а не константа: тесты (в т.ч. в internal/httpserver) подменяют
// её на единицы миллисекунд на время теста, чтобы не ждать реальный сон;
// боевое поведение (2 секунды) не меняется.
var RetryDelay = 2 * time.Second

type Client struct {
	Dir          string   // корень хранилища (AUDIO_DIR)
	AllowedHosts []string // суффиксы доменов; пусто — любой публичный хост
	MaxBytes     int64    // 0 → MaxAudioBytes
}

// ObjectInfo — то, что вызывающим нужно знать о сохранённом объекте.
type ObjectInfo struct {
	Size int64
}

// New создаёт каталог хранилища, если его нет; allowlist — из AUDIO_ALLOWED_HOSTS.
func New(dir string, allowedHosts []string) (*Client, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage dir: %w", err)
	}
	return &Client{Dir: dir, AllowedHosts: allowedHosts}, nil
}

// path переводит ключ в путь внутри Dir. Ключи строит сам сервер, но проверка
// на выход за корень дешевле, чем разбор, откуда ключ мог прийти.
func (c *Client) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" {
		return "", ErrBadKey
	}
	return filepath.Join(c.Dir, clean), nil
}

// dialGuarded резолвит адрес сам и отказывается соединяться с непубличным IP.
// Проверка живёт в диалере, а не в разборе URL: так она срабатывает и на
// редиректе, и на DNS-rebinding (домен резолвится в 127.0.0.1).
func dialGuarded(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	return dialFirstPublic(ctx, network, ips, port)
}

// tcpDial — фактическое TCP-соединение, переменная уровня пакета для подмены в
// тестах (без реальной сети).
var tcpDial = func(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// dialFirstPublic пробует резолвленные адреса по очереди и соединяется с первым
// публичным, до которого удалось дозвониться — как обычный HTTP-клиент делает
// при нескольких A/AAAA-записях у CDN. Каждый адрес проверяется isPublicIP
// отдельно: приватный адрес в списке просто пропускается, а не блокирует весь
// перебор. Возвращает последнюю осмысленную ошибку, если ни один адрес не
// сработал.
// AllowPrivateAudioTargets разрешает загрузчику ходить на приватные адреса.
//
// ЭТО ЗАЩИТА ОТ SSRF (T-007), и включать её на проде нельзя: колбэк Kie.ai
// присылает адрес, который сервер скачивает сам, — с открытым флагом подменённый
// колбэк дотянулся бы до 169.254.169.254 и до соседей по docker-сети.
//
// Флаг существует ровно для локального стенда (scripts/stand.sh full): сэмпл
// трека там отдаёт заглушка на 127.0.0.1, и без этого послабления путь
// «колбэк → скачивание → MinIO → прокси-ссылка» не проверить вообще — а именно
// в нём живут дыры, которые прод-режим и должен ловить. Ставится только из
// main по env AUDIO_ALLOW_PRIVATE=1, дефолт — выключено.
var AllowPrivateAudioTargets bool

func dialFirstPublic(ctx context.Context, network string, ips []net.IPAddr, port string) (net.Conn, error) {
	var lastErr error
	for _, ip := range ips {
		if !isPublicIP(ip.IP) && !AllowPrivateAudioTargets {
			lastErr = fmt.Errorf("%w: %s", ErrBlockedAddress, ip.IP)
			continue
		}
		conn, err := tcpDial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: no addresses resolved", ErrBlockedAddress)
	}
	return nil, lastErr
}

// isPublicIP: !IsLoopback && !IsPrivate && !IsLinkLocalUnicast &&
// !IsLinkLocalMulticast && !IsUnspecified && !IsMulticast (net.IP.IsPrivate
// покрывает 10/8, 172.16/12, 192.168/16 и fc00::/7).
func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

// hostAllowed проверяет суффикс домена, а не подстроку: evil-kie.ai не должен
// пройти под kie.ai.
func hostAllowed(host string, allowed []string) bool {
	if len(allowed) == 0 {
		return true // allowlist не задан: полагаемся на запрет приватных адресов
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "."))
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}

// Force HTTP/1.1 to avoid Cloudflare bot detection on Go's HTTP/2 TLS fingerprint.
// DialContext резолвит адрес и отказывается соединяться с непубличным IP —
// защита от SSRF, которая переживает редиректы и DNS-rebinding.
var downloadClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		DialContext:  dialGuarded,
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	},
}

// DownloadAndStore скачивает srcURL в память и кладёт файлом под key.
// Скачивание повторяется один раз: CDN Kie.ai часто отвечает медленно.
func (c *Client) DownloadAndStore(ctx context.Context, key, srcURL, contentType string) error {
	var buf []byte
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		buf, lastErr = c.downloadToMemory(ctx, srcURL)
		if lastErr == nil {
			break
		}
		if attempt < 2 {
			time.Sleep(RetryDelay)
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return c.Put(ctx, key, buf, contentType)
}

func (c *Client) downloadToMemory(ctx context.Context, srcURL string) ([]byte, error) {
	u, err := url.Parse(srcURL)
	if err != nil {
		return nil, fmt.Errorf("download parse url: %w", err)
	}
	if !hostAllowed(u.Hostname(), c.AllowedHosts) {
		return nil, fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ai-songs-bot/1.0)")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}

	limit := c.MaxBytes
	if limit <= 0 {
		limit = MaxAudioBytes
	}
	if resp.ContentLength > 0 && resp.ContentLength > limit {
		return nil, fmt.Errorf("%w: content-length %d", ErrTooLarge, resp.ContentLength)
	}

	buf, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download read: %w", err)
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("%w: >%d bytes", ErrTooLarge, limit)
	}

	log.Printf("[audio] download host=%s bytes=%d url=%s", u.Host, len(buf), srcURL)
	return buf, nil
}

// GetObject открывает объект под key на чтение.
func (c *Client) GetObject(_ context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	p, err := c.path(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, &ObjectInfo{Size: st.Size()}, nil
}

// Put записывает байты под ключ key: сначала во временный файл, потом rename —
// читатель (прокси трека) никогда не увидит недописанный mp3.
func (c *Client) Put(_ context.Context, key string, data []byte, _ string) error {
	p, err := c.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// GetBytes читает объект целиком, не больше MaxAudioBytes. Нарезка отрывка
// работает по всему файлу (обход фреймов), потоком её не сделать.
func (c *Client) GetBytes(ctx context.Context, key string) ([]byte, error) {
	obj, info, err := c.GetObject(ctx, key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	if info.Size > MaxAudioBytes {
		return nil, fmt.Errorf("%w: >%d bytes", ErrTooLarge, MaxAudioBytes)
	}
	return io.ReadAll(io.LimitReader(obj, MaxAudioBytes))
}
