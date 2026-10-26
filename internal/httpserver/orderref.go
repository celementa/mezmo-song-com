package httpserver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"mezmo-song-com/internal/store"
)

// Публичный идентификатор заказа: "<id>-<16 hex от HMAC-SHA256(WEBHOOK_SECRET, id)>".
// Клиенту и фронту виден только он; сырой id остаётся внутренним (первичный ключ,
// metadata.order_id в ЮKassa, ?orderId= в колбэке Kie.ai).
//
// orderRefCutoff — граница обратной совместимости. Заказы, созданные раньше, открываются
// и по голому id: ссылки на них уже уехали в письмах клиентам. Дата ставится на день ПОСЛЕ
// планового деплоя, чтобы ни одно разосланное письмо не отвалилось.
var orderRefCutoff = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

// orderRefSigHexLen — длина hex-подписи в символах: 8 байт HMAC-SHA256 = 16 hex.
// Усечение до 64 бит достаточно против перебора (см. Manual DoD), но не даёт лишней
// длины в URL.
const orderRefSigHexLen = 16

// orderRefSignature считает подпись id укороченным HMAC-SHA256.
func orderRefSignature(secret, id string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum[:orderRefSigHexLen/2])
}

// publicOrderRef собирает публичный идентификатор заказа.
func publicOrderRef(secret, id string) string {
	return id + "-" + orderRefSignature(secret, id)
}

// resolveOrderRef разбирает публичный идентификатор. ok=false — подпись не сходится
// (или строка не похожа на ref); в БД по такому значению ходить не нужно.
//
// Сырой id — hex без дефисов, поэтому разбор по последнему дефису однозначен.
func resolveOrderRef(secret, ref string) (id string, ok bool) {
	i := strings.LastIndex(ref, "-")
	if i <= 0 || i == len(ref)-1 {
		return "", false
	}
	candidateID, sig := ref[:i], ref[i+1:]
	if len(sig) != orderRefSigHexLen {
		return "", false
	}
	expected := orderRefSignature(secret, candidateID)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}
	return candidateID, true
}

// resolveOrderID — первая строка каждой публичной ручки заказа: превращает значение из
// пути в реальный id заказа.
//
//  1. ref с сходящейся подписью → id (основной путь);
//  2. голый id заказа, созданного раньше orderRefCutoff → id (старые письма);
//  3. иначе ok=false → ручка отвечает 404 и ничего не делает.
//
// Строка с дефисом, но неверной подписью, не попадает в п.2: сырой id всегда чистый
// hex без дефисов, значит это не легаси-ссылка, и ходить за ней в БД незачем — иначе
// ручка стала бы оракулом существования id по времени ответа/содержимому.
func (s *Server) resolveOrderID(ctx context.Context, ref string) (id string, ok bool) {
	if id, ok := resolveOrderRef(s.Config.WebhookSecret, ref); ok {
		return id, true
	}
	if strings.Contains(ref, "-") {
		return "", false
	}
	o, err := store.GetOrder(ctx, s.DB, ref)
	if err != nil {
		return "", false
	}
	if !o.CreatedAt.Before(orderRefCutoff) {
		return "", false
	}
	return o.ID, true
}

// publicOrderURL — ссылка на страницу заказа для писем и return_url.
func (s *Server) publicOrderURL(id string) string {
	lang := store.OrderLanguage(context.Background(), s.DB, id)
	return s.siteURL(lang) + "/order/" + publicOrderRef(s.Config.WebhookSecret, id)
}

// siteURL — корень сайта на языке lang без завершающего слэша: английская
// версия живёт под /en (frontend/src/lib/lang.ts).
func (s *Server) siteURL(lang string) string {
	base := strings.TrimSuffix(s.Config.PublicAppURL, "/")
	if lang == store.LangEN {
		return base + "/en"
	}
	return base
}
