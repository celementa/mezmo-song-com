package httpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// Каждый маршрут из router.tsx должен открываться со статусом 200, а каждый
// маршрут из списков Go — существовать во фронте: иначе новая страница отдаст
// 404, а удалённая — 200.
func TestSPARoutesMatchRouter(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/router.tsx")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, m := range regexp.MustCompile(`path: '([^']*)'`).FindAllStringSubmatch(string(src), -1) {
		paths = append(paths, strings.ReplaceAll(m[1], ":orderId", "ord_1"))
	}
	if len(paths) < 20 {
		t.Fatalf("из router.tsx прочитано %d маршрутов — разбор сломался", len(paths))
	}
	for _, p := range paths {
		if !spaRouteKnown(p) {
			t.Errorf("маршрут фронта %s отдал бы 404", p)
		}
	}
	all := append(append(append([]string{}, spaContentPaths...), spaSubscriptionPaths...), spaAdminPaths...)
	for _, p := range all {
		if !strings.Contains(string(src), "path: '"+p+"'") {
			t.Errorf("маршрута %s нет в router.tsx — уберите его из spa.go", p)
		}
	}
}

func TestSPARouteKnown(t *testing.T) {
	for p, want := range map[string]bool{
		"/": true, "/ru": true, "/en/": true,
		"/ru/config": true, "/en/terms": true, "/config": true,
		"/en/order/ord_1": true, "/order/ord_1": true, "/order/": false, "/order/a/b": false,
		"/ru/subscribe": true, "/subscribe": true, "/en/subscribe": false,
		"/en/account": true, "/en/subscription-offer": false,
		"/admin/orders": true, "/nope": false, "/en/nope": false, "/sitemap.xml": false,
	} {
		if got := spaRouteKnown(p); got != want {
			t.Errorf("spaRouteKnown(%q) = %v, want %v", p, got, want)
		}
	}
}

// Все заменяемые строки должны найтись в index.html: правка head без правки
// spa.go вернула бы /en русское превью.
func TestLocalizeIndexEN(t *testing.T) {
	src, err := os.ReadFile("../../frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range spaEnReplacements {
		if !strings.Contains(string(src), r[0]) {
			t.Errorf("в index.html нет %q", r[0])
		}
	}
	en := string(localizeIndex(src, "/en/config"))
	for _, bad := range []string{`<html lang="ru">`, "ru_RU", spaTitleRu, spaDescriptionRu} {
		if strings.Contains(en, bad) {
			t.Errorf("в EN-head осталось %q", bad)
		}
	}
	if string(localizeIndex(src, "/ru")) != string(src) || string(localizeIndex(src, "/english")) != string(src) {
		t.Error("не-EN адрес получил изменённый index.html")
	}
}

func TestSPAHandlerStatus(t *testing.T) {
	h := SPAHandler(fstest.MapFS{
		"index.html":    {Data: []byte(`<html lang="ru"><title>` + spaTitleRu + `</title>`)},
		"assets/app.js": {Data: []byte("js")},
	})
	for _, c := range []struct {
		path, wantBody string
		status         int
	}{
		{"/ru", `lang="ru"`, 200},
		{"/en/config", spaTitleEn, 200},
		{"/en/nope", spaTitleEn, 404},
		{"/sitemap.xml", `lang="ru"`, 404},
		{"/assets/app.js", "js", 200},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.wantBody) {
			t.Errorf("%s: %d %q, want %d с %q", c.path, rec.Code, rec.Body.String(), c.status, c.wantBody)
		}
	}
}
