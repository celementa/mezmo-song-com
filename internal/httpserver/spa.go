package httpserver

import (
	"bytes"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Маршруты SPA (frontend/src/router.tsx). Go знает их, чтобы на неизвестный
// адрес ответить настоящим 404, а не index.html со статусом 200. Сверку со
// списком маршрутов фронта держит TestSPARoutesMatchRouter.
var (
	// contentPages: открываются без префикса, под /ru и под /en.
	spaContentPaths = []string{
		"/start", "/config", "/create", "/thanks",
		"/pay/stripe", "/pay/yookassa", "/pay/unitpay",
		"/terms", "/privacy", "/security", "/consent", "/contacts",
	}
	// subscriptionPages: без префикса и под /ru; /en/subscribe нет.
	spaSubscriptionPaths = []string{
		"/subscribe", "/subscribe/return", "/account", "/account/new",
		"/subscription/cancel", "/subscription-offer",
	}
	// EN-кабинет и отмена подписки.
	spaEnSubscriptionPaths = []string{"/account", "/account/new", "/subscription/cancel"}
	// Страницы админки (их Go отдаёт только после входа, см. cmd/server/adminauth.go).
	spaAdminPaths = []string{"/admin/orders", "/admin/analytics", "/admin/settings", "/admin/emails"}
)

// spaRouteKnown — есть ли у SPA страница по этому адресу.
func spaRouteKnown(p string) bool {
	p = strings.TrimSuffix(p, "/")
	if p == "" || p == "/ru" || p == "/en" || slices.Contains(spaAdminPaths, p) {
		return true
	}
	lang, rest := "", p
	if strings.HasPrefix(p, "/ru/") || strings.HasPrefix(p, "/en/") {
		lang, rest = p[1:3], p[3:]
	}
	if slices.Contains(spaContentPaths, rest) {
		return true
	}
	if id, ok := strings.CutPrefix(rest, "/order/"); ok && id != "" && !strings.Contains(id, "/") {
		return true
	}
	if lang == "en" {
		return slices.Contains(spaEnSubscriptionPaths, rest)
	}
	return slices.Contains(spaSubscriptionPaths, rest)
}

// Английский <head> для /en. index.html собирается один, с русским head, и его
// заголовок и описание SPA меняет уже в браузере — а превью ссылок в мессенджерах
// и рекламе Meta читают HTML без JS. Тексты — те же, что в PAGE_META
// (frontend/src/features/analytics/PublicRoot.tsx).
const (
	spaTitleRu       = "Песня на заказ - подарок, который не забудут"
	spaTitleEn       = "A custom song - the gift they will never forget"
	spaDescriptionRu = "Превратите вашу историю в музыку. Наш сервис создаст идеальный подарок на день рождения, свадьбу или юбилей. Быстро, уникально, навсегда."
	spaDescriptionEn = "Turn your story into music. A personalized song for a birthday, wedding or anniversary. Fast, unique and yours forever."
)

// spaEnReplacements — пары «как в index.html → для /en». Каждая строка обязана
// найтись в index.html (TestLocalizeIndexEN), иначе правка вёрстки head молча
// вернула бы /en русское превью.
var spaEnReplacements = [][2]string{
	{`<html lang="ru">`, `<html lang="en">`},
	{spaTitleRu, spaTitleEn},
	{spaDescriptionRu, spaDescriptionEn},
	{`content="персональная песня, песня на заказ, музыка, подарок, кастомный трек"`, `content="personalized song, custom song, music, gift, song gift"`},
	{`<link rel="canonical" href="https://mezmo-song.com/ru/" />`, `<link rel="canonical" href="https://mezmo-song.com/en/" />`},
	{`content="ru_RU"`, `content="en_US"`},
	{`>Подарок, который трогает душу<`, `>A gift that touches the heart<`},
}

// localizeIndex отдаёт index.html под язык адреса: для /en — с английским head.
func localizeIndex(html []byte, path string) []byte {
	if path != "/en" && !strings.HasPrefix(path, "/en/") {
		return html
	}
	for _, r := range spaEnReplacements {
		html = bytes.ReplaceAll(html, []byte(r[0]), []byte(r[1]))
	}
	return html
}

// SPAHandler отдаёт файлы фронта из content, а на адреса страниц — index.html:
// с английским head под /en и со статусом 404 на неизвестный адрес (SPA при
// этом всё равно рисует свою страницу «не найдено»).
//
// index.html читается на каждый запрос: выкладка фронта меняет имена чанков,
// и закэшированный при старте index ссылался бы на удалённые файлы.
func SPAHandler(content fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(content))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(content, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		indexHTML, err := fs.ReadFile(content, "index.html")
		if err != nil {
			log.Printf("spa: index.html: %v", err)
			http.Error(w, "frontend not deployed", http.StatusServiceUnavailable)
			return
		}
		page := localizeIndex(indexHTML, r.URL.Path)
		w.Header().Set("Cache-Control", "no-cache")
		if p != "index.html" && !spaRouteKnown(r.URL.Path) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if r.Method == http.MethodGet {
				_, _ = w.Write(page)
			}
			return
		}
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page))
	})
}
