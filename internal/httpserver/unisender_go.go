package httpserver

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"mezmo-song-com/internal/store"
)

// Вебхук Unisender Go: что стало с письмом после отправки (доставлено,
// открыто, недоставка, спам). Письмо находится по metadata.ref — ключу,
// который sendEmail кладёт и в заголовок X-UNISENDER-GO, и в email_log.
//
// Подключается разово вызовом webhook/set (scripts/unisender-go-webhook.sh) с
// delivery_info=1 — без него в событии «открыто» нет traffic_type, и
// человеческое открытие не отличить от предзагрузки Apple Mail.

// randomRef — ключ письма для Go (16 hex).
func randomRef() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type unisenderGoCallback struct {
	Auth         string `json:"auth"`
	EventsByUser []struct {
		Events []struct {
			EventName string `json:"event_name"`
			EventData struct {
				Metadata     map[string]any `json:"metadata"`
				Status       string         `json:"status"`
				EventTime    string         `json:"event_time"`
				DeliveryInfo struct {
					TrafficType string `json:"traffic_type"`
				} `json:"delivery_info"`
			} `json:"event_data"`
		} `json:"events"`
	} `json:"events_by_user"`
}

// unisenderGoAuthOK — проверка по документации Go: auth = MD5 тела, в котором
// значение auth заменено на API-ключ. Ключ тот же, что пароль SMTP Go.
func unisenderGoAuthOK(raw []byte, auth, key string) bool {
	if auth == "" || key == "" {
		return false
	}
	sum := md5.Sum([]byte(strings.Replace(string(raw), auth, key, 1)))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(auth))) == 1
}

func (s *Server) handleUnisenderGoWebhook(w http.ResponseWriter, r *http.Request) {
	// webhook/set проверяет адрес GET-запросом без параметров и ждёт 200.
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusOK)
		return
	}
	key := ""
	if s.Config.SMTPUnisenderGo {
		key = s.Config.SMTPPassword
	}
	if key == "" {
		log.Printf("[mail-go] webhook: SMTP_UNISENDER_GO не включён — событие не проверить")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var cb unisenderGoCallback
	if err := json.Unmarshal(raw, &cb); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !unisenderGoAuthOK(raw, cb.Auth, key) {
		log.Printf("[mail-go] webhook: неверная подпись auth")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	applied, unknown := 0, 0
	for _, u := range cb.EventsByUser {
		for _, e := range u.Events {
			if e.EventName != "transactional_email_status" {
				continue
			}
			ref, _ := e.EventData.Metadata["ref"].(string)
			if ref == "" {
				continue
			}
			at := time.Now().UTC()
			if t, err := time.Parse("2006-01-02 15:04:05", e.EventData.EventTime); err == nil {
				at = t
			}
			human := e.EventData.DeliveryInfo.TrafficType == "human"
			ok, err := store.ApplyEmailEvent(r.Context(), s.DB, ref, e.EventData.Status, at.Format(time.RFC3339), human)
			if err != nil {
				// 500 — Go повторит вызов позже; события идемпотентны.
				log.Printf("[mail-go] webhook: ref=%s status=%s err=%v", ref, e.EventData.Status, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if ok {
				applied++
			} else {
				unknown++
			}
		}
	}
	if unknown > 0 {
		log.Printf("[mail-go] webhook: applied=%d unknown_ref=%d", applied, unknown)
	}
	w.WriteHeader(http.StatusOK)
}
