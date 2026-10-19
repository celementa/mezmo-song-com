package httpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/storage"
	"mezmo-song-com/internal/store"
)

// Если DownloadAndStore не смог скачать трек (например, allowlist заблокировал
// хост Kie.ai), заказ обязан получить исходный URL Kie.ai, а не демо-фолбэк —
// гард против регрессии в обработке ошибки загрузчика (internal/storage).
func TestKieWebhookKeepsSourceURLWhenStoreFails(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// DownloadAndStore ретраит с паузой storage.RetryDelay (2с в проде) — в тесте
	// сокращаем её почти до нуля, иначе фоновая горутина не успевает уложиться в
	// дедлайн ожидания ниже при параллельном прогоне пакетов (флак).
	origRetryDelay := storage.RetryDelay
	storage.RetryDelay = time.Millisecond
	t.Cleanup(func() { storage.RetryDelay = origRetryDelay })

	o := store.Order{
		ID:               "ord_kie",
		Status:           "processing",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    "paid",
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{"email":""}`,
		BasePrice:        990,
		CreatedAt:        time.Now(),
	}
	if err := store.CreateOrder(context.Background(), db, o); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Allowlist заведомо не пропускает хост трека — DownloadAndStore вернёт
	// ErrHostNotAllowed без единого сетевого запроса.
	sc, err := storage.New(t.TempDir(), []string{"only-allowed.invalid"})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	s := &Server{DB: db, Config: config.Config{WebhookSecret: "secret"}, Storage: sc}

	const trackURL = "https://cdn.kie.ai/track-1.mp3"
	body, _ := json.Marshal(map[string]any{
		"code": 200,
		"data": map[string]any{
			"callbackType": "complete",
			"data": []map[string]any{
				{"audio_url": trackURL, "title": "Song"},
			},
		},
	})

	req := httptest.NewRequest("POST", "/api/webhooks/kie?orderId=ord_kie&secret=secret", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.handleKieWebhook(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Сохранение результата идёт в фоновой горутине — ждём с таймаутом.
	deadline := time.Now().Add(2 * time.Second)
	var got store.Order
	for {
		got, err = store.GetOrder(context.Background(), db, "ord_kie")
		if err != nil {
			t.Fatalf("get order: %v", err)
		}
		if got.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for kie webhook goroutine to save result")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var urls []string
	if err := json.Unmarshal([]byte(got.AudioURLsJSON), &urls); err != nil {
		t.Fatalf("unmarshal audio urls: %v", err)
	}
	if len(urls) != 1 || urls[0] != trackURL {
		t.Fatalf("audio_urls = %v, want [%s] (исходный URL Kie.ai при ошибке загрузки в MinIO)", urls, trackURL)
	}
	if store.IsFallbackAudio(got.AudioURLsJSON) {
		t.Fatal("audio считается демо-фолбэком, но загрузка должна была просто оставить исходный URL Kie.ai")
	}
}

// taskId и audioId из колбэка обязаны попасть в заказ: без них недоступны
// replace-section и Persona, а восстановить их потом нечем — повторного колбэка
// по задаче не будет. Проверяется на том же сценарии упавшей выгрузки в MinIO:
// идентификаторы не должны зависеть от того, доехал ли файл до хранилища.
func TestKieWebhookStoresTaskAndAudioIDs(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	origRetryDelay := storage.RetryDelay
	storage.RetryDelay = time.Millisecond
	t.Cleanup(func() { storage.RetryDelay = origRetryDelay })

	o := store.Order{
		ID:               "ord_kie_ids",
		Status:           "processing",
		DeliveryStatus:   store.DeliveryPending,
		PaymentStatus:    "paid",
		InputPayloadJSON: `{"recipientName":"Ира"}`,
		CustomerJSON:     `{"email":""}`,
		BasePrice:        990,
		CreatedAt:        time.Now(),
	}
	if err := store.CreateOrder(context.Background(), db, o); err != nil {
		t.Fatalf("create: %v", err)
	}

	sc, err := storage.New(t.TempDir(), []string{"only-allowed.invalid"})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	s := &Server{DB: db, Config: config.Config{WebhookSecret: "secret"}, Storage: sc}

	body, _ := json.Marshal(map[string]any{
		"code": 200,
		"data": map[string]any{
			"callbackType": "complete",
			"task_id":      "task_abc",
			"data": []map[string]any{
				{"id": "audio_1", "audio_url": "https://cdn.kie.ai/track-1.mp3", "title": "Song"},
				{"id": "audio_2", "audio_url": "https://cdn.kie.ai/track-2.mp3", "title": "Song"},
			},
		},
	})

	req := httptest.NewRequest("POST", "/api/webhooks/kie?orderId=ord_kie_ids&secret=secret", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.handleKieWebhook(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got store.Order
	for {
		got, err = store.GetOrder(context.Background(), db, "ord_kie_ids")
		if err != nil {
			t.Fatalf("get order: %v", err)
		}
		if got.KieTaskID.Valid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for kie ids to be saved")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got.KieTaskID.String != "task_abc" {
		t.Fatalf("kie_task_id = %q, want task_abc", got.KieTaskID.String)
	}
	var audioIDs []string
	if err := json.Unmarshal([]byte(got.KieAudioIDs.String), &audioIDs); err != nil {
		t.Fatalf("unmarshal kie_audio_ids (%q): %v", got.KieAudioIDs.String, err)
	}
	if len(audioIDs) != 2 || audioIDs[0] != "audio_1" || audioIDs[1] != "audio_2" {
		t.Fatalf("kie_audio_ids = %v, want [audio_1 audio_2]", audioIDs)
	}
}
