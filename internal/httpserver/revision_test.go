package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/store"
)

// Текст, присланный на доработке, обязан попасть в песню дословно. Раньше он
// подставлялся в story, уходил в промпт генерации и модель писала по нему свой
// текст — «вставляю текст конкретно, что мне нужно, а в песне снова другое».
//
// Ключей OpenRouter и Kie.ai в тесте нет, поэтому доработка завершается
// демо-фолбэком — но текст песни в БД показывает, что именно ушло бы в Kie.ai:
// при старом поведении генерация без ключа возвращала пустую строку и заказ
// получал демо-текст вместо присланного.
func TestRegenerateKeepsCustomLyricsVerbatim(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_rev1", "paid")

	const custom = "[Куплет 1]\nМой собственный текст\n\n[Припев]\nИменно эти слова"
	body := `{"revisionType":"lyrics","newLyrics":` + jsonQuote(custom) + `}`

	req := httptest.NewRequest("POST", "/api/public/order/ord_rev1/regenerate", strings.NewReader(body))
	req.SetPathValue("id", "ord_rev1")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	got := waitForLyrics(t, s, "ord_rev1")
	if got != custom {
		t.Fatalf("lyrics_text = %q, want присланный текст без изменений %q", got, custom)
	}
}

// Доработка без своего текста (правки словами) по-прежнему идёт через генерацию:
// присланного текста нет, переписывать нечего — этот путь трогать не должны.
func TestRegenerateWithoutCustomLyricsStillGenerates(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_rev2", "paid")

	req := httptest.NewRequest("POST", "/api/public/order/ord_rev2/regenerate",
		strings.NewReader(`{"revisionType":"describe","description":"сделай припев короче"}`))
	req.SetPathValue("id", "ord_rev2")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// Без ключа OpenRouter генерации не происходит и заказ закрывается демо-текстом.
	// Здесь важно лишь то, что инструкция правки не подставилась вместо текста песни.
	got := waitForLyrics(t, s, "ord_rev2")
	if strings.Contains(got, "сделай припев короче") {
		t.Fatalf("lyrics_text = %q: инструкция правки попала в текст песни", got)
	}
}

// Прошлые версии текста должны доезжать до страницы заказа: иначе история есть в
// БД, но вернуть удачный вариант клиенту всё равно нечем.
func TestPublicOrderContentExposesLyricsVersions(t *testing.T) {
	s := orderRefTestServer(t)
	ctx := context.Background()
	seedOrder(t, s.DB, "ord_rev3", "paid")

	if err := store.SaveRelayResult(ctx, s.DB, "ord_rev3",
		[]string{audioObjectKey("ord_rev3", 0, 1)}, "первый текст", ""); err != nil {
		t.Fatalf("save relay result: %v", err)
	}
	if _, err := store.IncrementRegeneration(ctx, s.DB, "ord_rev3"); err != nil {
		t.Fatalf("increment regeneration: %v", err)
	}
	if err := store.SaveRelayResult(ctx, s.DB, "ord_rev3",
		[]string{audioObjectKey("ord_rev3", 1, 1)}, "второй текст", ""); err != nil {
		t.Fatalf("save relay result after revision: %v", err)
	}

	ref := publicOrderRef(s.Config.WebhookSecret, "ord_rev3")
	req := httptest.NewRequest("GET", "/api/public/order/"+ref+"/content", nil)
	req.SetPathValue("id", ref)
	rec := httptest.NewRecorder()
	s.handlePublicOrderContent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Orders []struct {
			LyricsText     string `json:"lyricsText"`
			LyricsVersions []struct {
				Version    int    `json:"version"`
				LyricsText string `json:"lyricsText"`
			} `json:"lyricsVersions"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Orders) != 1 {
		t.Fatalf("unexpected orders payload: %s", rec.Body.String())
	}
	if len(out.Orders[0].LyricsVersions) != 1 {
		t.Fatalf("версий в ответе = %d, want 1: %s", len(out.Orders[0].LyricsVersions), rec.Body.String())
	}
	if !strings.Contains(out.Orders[0].LyricsVersions[0].LyricsText, "первый текст") {
		t.Fatalf("в истории не первый текст: %q", out.Orders[0].LyricsVersions[0].LyricsText)
	}
	if !strings.Contains(out.Orders[0].LyricsText, "второй текст") {
		t.Fatalf("текущий текст не второй: %q", out.Orders[0].LyricsText)
	}
}

// Просьба сменить голос словами в описании правки должна менять пол вокала и
// запоминаться в заказе. Реальная жалоба: «мужской голос» в поле правок
// переписывал текст в мужском роде, но песня продолжала петься женским —
// пол вокала считался только из дропдауна и не сохранялся между доработками.
func TestRegenerateVoiceRequestInDescriptionPersists(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_rev4", "paid")

	req := httptest.NewRequest("POST", "/api/public/order/ord_rev4/regenerate",
		strings.NewReader(`{"revisionType":"describe","description":"сделайте мужской голос"}`))
	req.SetPathValue("id", "ord_rev4")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(context.Background(), s.DB, "ord_rev4")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	var payload struct {
		MusicPreferences struct {
			VoiceStyle string `json:"voiceStyle"`
		} `json:"musicPreferences"`
	}
	if err := json.Unmarshal([]byte(o.InputPayloadJSON), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.MusicPreferences.VoiceStyle != "male" {
		t.Fatalf("voiceStyle = %q, want %q (просьба из описания не сохранилась)",
			payload.MusicPreferences.VoiceStyle, "male")
	}
	waitForLyrics(t, s, "ord_rev4")
}

// Выбор голоса дропдауном тоже должен переживать доработку: раньше следующая
// правка без явного выбора откатывалась к voiceStyle исходного заказа.
func TestRegenerateVoiceDropdownPersists(t *testing.T) {
	s := orderRefTestServer(t)
	seedOrder(t, s.DB, "ord_rev5", "paid")

	req := httptest.NewRequest("POST", "/api/public/order/ord_rev5/regenerate",
		strings.NewReader(`{"revisionType":"describe","description":"сделай веселее","voiceStyle":"male"}`))
	req.SetPathValue("id", "ord_rev5")
	rec := httptest.NewRecorder()
	s.handleRegenerate(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	o, err := store.GetOrder(context.Background(), s.DB, "ord_rev5")
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if !strings.Contains(o.InputPayloadJSON, `"voiceStyle":"male"`) {
		t.Fatalf("input_payload = %s: выбор дропдауна не сохранился", o.InputPayloadJSON)
	}
	waitForLyrics(t, s, "ord_rev5")
}

// waitForLyrics ждёт, пока фоновая горутина доработки запишет текст песни.
func waitForLyrics(t *testing.T, s *Server, id string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		o, err := store.GetOrder(context.Background(), s.DB, id)
		if err != nil {
			t.Fatalf("get order: %v", err)
		}
		if o.LyricsText.Valid && o.LyricsText.String != "" {
			return o.LyricsText.String
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for regenerate goroutine to save lyrics")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// jsonQuote — строка как JSON-литерал, чтобы переносы строк в тексте песни не
// ломали тело запроса.
func jsonQuote(s string) string {
	return `"` + strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\n", "\\n").Replace(s) + `"`
}
