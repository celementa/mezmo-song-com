package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"mezmo-song-com/internal/store"
)

// Ключ трека обязан меняться от прогона к прогону: иначе доработка перезаписывает
// объект в MinIO, ссылка остаётся прежней и кэш браузера продолжает отдавать
// прошлую песню.
func TestAudioObjectKeyIsScopedToRegeneration(t *testing.T) {
	first := audioObjectKey("ord_x", 0, 1)
	afterRevision := audioObjectKey("ord_x", 1, 1)

	if first == afterRevision {
		t.Fatalf("ключ не зависит от номера прогона: %q", first)
	}
	// Префикс orders/ — условие, по которому handleAudioProxy отличает наш объект
	// в MinIO от внешнего URL (демо-трек), и по которому content-ручка решает,
	// заворачивать ли ссылку в прокси.
	for _, key := range []string{first, afterRevision} {
		if !strings.HasPrefix(key, "orders/ord_x/") {
			t.Fatalf("ключ %q вне префикса orders/{id}/ — прокси его не отдаст", key)
		}
	}
}

// Публичная ссылка на трек тоже обязана меняться после доработки. Это и есть
// защита от жалобы «текст исправился, а песня поётся старая»: с прежним URL
// браузер брал mp3 из своего кэша, не спрашивая сервер.
func TestPublicOrderContentAudioURLChangesAfterRegeneration(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_cache1", "paid")

	if err := store.SaveRelayResult(ctx, s.DB, "ord_cache1",
		[]string{audioObjectKey("ord_cache1", 0, 1)}, "первый текст", ""); err != nil {
		t.Fatalf("save relay result: %v", err)
	}
	before := contentTrack(t, s, "ord_cache1")

	// Доработка: счётчик прогонов растёт, приезжает новая песня.
	if _, err := store.IncrementRegeneration(ctx, s.DB, "ord_cache1"); err != nil {
		t.Fatalf("increment regeneration: %v", err)
	}
	if err := store.SaveRelayResult(ctx, s.DB, "ord_cache1",
		[]string{audioObjectKey("ord_cache1", 1, 1)}, "исправленный текст", ""); err != nil {
		t.Fatalf("save relay result after revision: %v", err)
	}
	after := contentTrack(t, s, "ord_cache1")

	if before == after {
		t.Fatalf("ссылка на трек после доработки не изменилась (%q) — браузер отдаст прошлую песню из кэша", before)
	}
}

// contentTrack возвращает единственную ссылку на трек из GET /content.
func contentTrack(t *testing.T, s *Server, id string) string {
	t.Helper()
	ref := publicOrderRef(s.Config.WebhookSecret, id)

	req := httptest.NewRequest("GET", "/api/public/order/"+ref+"/content", nil)
	req.SetPathValue("id", ref)
	rec := httptest.NewRecorder()
	s.handlePublicOrderContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Orders []struct {
			Tracks []string `json:"tracks"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Orders) != 1 || len(out.Orders[0].Tracks) != 1 {
		t.Fatalf("unexpected orders payload: %s", rec.Body.String())
	}
	return out.Orders[0].Tracks[0]
}
