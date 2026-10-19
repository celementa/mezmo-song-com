package storage

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// withPlainClient временно заменяет package-level downloadClient на клиента без
// dialGuarded, чтобы изолированно тестировать hostAllowed/лимиты без реальной
// сети (httptest-сервер сам живёт на loopback, который guard иначе заблокирует).
func withPlainClient(t *testing.T) {
	t.Helper()
	orig := downloadClient
	downloadClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { downloadClient = orig })
}

// withRedirectingClient заменяет downloadClient на транспорт, который для
// addr == pinnedAddr соединяется с реальным тестовым сервером напрямую, а для
// любого другого адреса зовёт настоящий dialGuarded. Это позволяет проверить
// хостовую allowlist-проверку по фиктивному имени хоста, не завязываясь на
// реальный DNS, и одновременно проверить, что guard реально работает на каждом
// соединении (в т.ч. при редиректе).
func withRedirectingClient(t *testing.T, pinnedAddr, realAddr string) {
	t.Helper()
	orig := downloadClient
	downloadClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr == pinnedAddr {
					return net.Dial(network, realAddr)
				}
				return dialGuarded(ctx, network, addr)
			},
		},
	}
	t.Cleanup(func() { downloadClient = orig })
}

func TestDownloadRejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("сервер не должен получить запрос: адрес заблокирован до соединения")
	}))
	defer srv.Close()

	c := &Client{}
	_, err := c.downloadToMemory(context.Background(), srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestDownloadRejectsLinkLocalMetadata(t *testing.T) {
	c := &Client{}
	_, err := c.downloadToMemory(context.Background(), "http://169.254.169.254/latest/meta-data/")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestDownloadRejectsHostNotInAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("сервер не должен получить запрос: хост не проходит allowlist")
	}))
	defer srv.Close()

	c := &Client{AllowedHosts: []string{"kie.ai"}}
	_, err := c.downloadToMemory(context.Background(), srv.URL)
	if !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("err = %v, want ErrHostNotAllowed", err)
	}
}

func TestDownloadAllowsHostInAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("audio-bytes"))
	}))
	defer srv.Close()

	orig := downloadClient
	t.Cleanup(func() { downloadClient = orig })
	realAddr := srv.Listener.Addr().String()
	downloadClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// Оба фиктивных хоста маршрутизируем на реальный тестовый сервер:
				// проверяется только логика hostAllowed, а не сеть.
				return net.Dial(network, realAddr)
			},
		},
	}

	c := &Client{AllowedHosts: []string{"kie.ai"}}

	if _, err := c.downloadToMemory(context.Background(), "http://cdn.kie.ai/track.mp3"); err != nil {
		t.Fatalf("cdn.kie.ai should be allowed as suffix of kie.ai: %v", err)
	}
	if _, err := c.downloadToMemory(context.Background(), "http://evil-kie.ai/track.mp3"); !errors.Is(err, ErrHostNotAllowed) {
		t.Fatalf("evil-kie.ai err = %v, want ErrHostNotAllowed (проверка суффикса, а не подстроки)", err)
	}
}

func TestDownloadEnforcesSizeLimit(t *testing.T) {
	const limit = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), limit*2))
	}))
	defer srv.Close()

	withPlainClient(t)

	c := &Client{MaxBytes: limit}
	_, err := c.downloadToMemory(context.Background(), srv.URL)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestDownloadRejectsContentLengthOverLimit(t *testing.T) {
	const limit = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(limit*10))
		w.WriteHeader(http.StatusOK)
		// Тело не пишем: ошибка обязана случиться до попытки его прочитать.
	}))
	defer srv.Close()

	withPlainClient(t)

	c := &Client{MaxBytes: limit}
	_, err := c.downloadToMemory(context.Background(), srv.URL)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// TestDialFirstPublicSkipsUnreachableFirstAddress проверяет перебор адресов:
// CDN может отдать несколько A-записей, и недоступность первой не должна
// обрушивать соединение целиком — обычный клиент идёт по следующей.
func TestDialFirstPublicSkipsUnreachableFirstAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	realAddr := srv.Listener.Addr().String()

	orig := tcpDial
	var dialed []string
	tcpDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		if strings.HasPrefix(address, "203.0.113.1:") {
			return nil, errors.New("connection refused")
		}
		return net.Dial(network, realAddr)
	}
	t.Cleanup(func() { tcpDial = orig })

	ips := []net.IPAddr{{IP: net.ParseIP("203.0.113.1")}, {IP: net.ParseIP("203.0.113.2")}}
	conn, err := dialFirstPublic(context.Background(), "tcp", ips, "80")
	if err != nil {
		t.Fatalf("dialFirstPublic() error = %v, want nil (second address should work)", err)
	}
	conn.Close()
	if len(dialed) != 2 {
		t.Fatalf("dialed addresses = %v, want 2 attempts (first failed, second succeeded)", dialed)
	}
}

// TestDialFirstPublicSkipsPrivateAddress проверяет, что приватный адрес в
// списке резолва не используется для соединения даже если он не первый.
func TestDialFirstPublicSkipsPrivateAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	realAddr := srv.Listener.Addr().String()

	orig := tcpDial
	var dialed []string
	tcpDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return net.Dial(network, realAddr)
	}
	t.Cleanup(func() { tcpDial = orig })

	ips := []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("203.0.113.2")}}
	conn, err := dialFirstPublic(context.Background(), "tcp", ips, "80")
	if err != nil {
		t.Fatalf("dialFirstPublic() error = %v, want nil (public address should work)", err)
	}
	conn.Close()
	for _, addr := range dialed {
		if strings.HasPrefix(addr, "10.0.0.1") {
			t.Fatalf("private address must not be dialed, dialed = %v", dialed)
		}
	}
	if len(dialed) != 1 {
		t.Fatalf("dialed addresses = %v, want exactly 1 (only the public one)", dialed)
	}
}

// TestDialFirstPublicAllPrivateBlocked проверяет, что если ВСЕ резолвленные
// адреса приватные, соединение не устанавливается вовсе (перебор не превращает
// проверку публичности в дырку).
func TestDialFirstPublicAllPrivateBlocked(t *testing.T) {
	orig := tcpDial
	tcpDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		t.Fatal("tcpDial не должен вызываться: все адреса приватные")
		return nil, nil
	}
	t.Cleanup(func() { tcpDial = orig })

	ips := []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}, {IP: net.ParseIP("192.168.1.1")}}
	_, err := dialFirstPublic(context.Background(), "tcp", ips, "80")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestDownloadFollowsRedirectWithChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/actual.mp3", http.StatusFound)
	}))
	defer srv.Close()

	// Пиним только исходный хост на реальный тестовый сервер; редирект на
	// 127.0.0.1:1 идёт через настоящий dialGuarded и обязан быть заблокирован —
	// именно на этом диалере, а не на разборе исходного URL.
	withRedirectingClient(t, "public.example.test:80", srv.Listener.Addr().String())

	c := &Client{}
	_, err := c.downloadToMemory(context.Background(), "http://public.example.test/track.mp3")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}
