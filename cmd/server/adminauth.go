package main

// Авторизация админского раздела.
//
// Исторически весь /admin* был закрыт HTTP Basic Auth, но его диалог рисует сам
// браузер, а встроенные браузеры Instagram и Telegram этот механизм не
// реализуют: пользователь получал 401 и голое "unauthorized". Поэтому вход
// сделан обычной HTML-формой — она одинаково работает на десктопе, на телефоне
// и в любом вебвью. Заголовок WWW-Authenticate больше не отправляется никогда,
// так что системный диалог не всплывает нигде.
//
// Роли:
//
//	admin    — ADMIN_USER / ADMIN_PASS, полный доступ к /admin* и /api/admin/*;
//	ads      — ADS_USER / ADS_PASS, только страница воронки и ручка создания
//	           заказа. Эти креды отдают модераторам рекламных площадок, поэтому
//	           заказы и аналитика с данными клиентов им недоступны;
//	lesroses — LESROSES_USER / LESROSES_PASS, кабинет партнёра Les Roses:
//	           только его заказы-апсейлы (/admin/lesroses и его API).
//
// Basic Auth продолжает приниматься, если его прислали явно (curl, скрипты),
// но браузер к нему сам уже не перейдёт.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"mezmo-song-com/internal/config"
)

const (
	roleAdmin = "admin"
	roleAds   = "ads"
	// roleLesroses — кабинет партнёра: видит только заказы, проданные в его магазине.
	roleLesroses = "lesroses"

	lesrosesPagePath = "/admin/lesroses"
	lesrosesAPIPath  = "/api/admin/lesroses/"

	adsPagePath   = "/admin/ads"
	adsDraftPath  = "/api/admin/orders/draft"
	loginPath     = "/admin/login"
	logoutPath    = "/admin/logout"
	adsTokenParam = "k"

	sessionCookie = "admin_session"
	sessionTTL    = 90 * 24 * time.Hour

	loginMaxAttempts = 10
	loginWindow      = 15 * time.Minute
)

// adsScoped reports whether a path belongs to the ad-access flow: the funnel page
// itself plus the single endpoint it posts to. Everything else under /admin stays
// admin-only, so leaked ad credentials can never reach customer data.
func adsScoped(p string) bool {
	return p == adsPagePath || p == adsDraftPath
}

// lesrosesScoped — страница кабинета Les Roses и её API.
func lesrosesScoped(p string) bool {
	return p == lesrosesPagePath || strings.HasPrefix(p, lesrosesAPIPath)
}

// sessionValue derives the cookie payload for a role from the secret that grants
// it. Changing the secret invalidates every session issued under it, and the raw
// password never travels in the cookie.
func sessionValue(secret, role string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(role))
	return hex.EncodeToString(mac.Sum(nil))
}

// sessionRole returns the role carried by the request's session cookie, or "".
func sessionRole(cfg config.Config, r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	match := func(secret, role string) bool {
		return secret != "" && subtle.ConstantTimeCompare([]byte(c.Value), []byte(sessionValue(secret, role))) == 1
	}
	switch {
	case match(cfg.AdminPass, roleAdmin):
		return roleAdmin
	case match(cfg.AdsPass, roleAds):
		return roleAds
	// Ссылка с токеном выдаёт ту же роль ads — отдельного механизма для неё нет.
	case match(cfg.AdsAccessToken, roleAds):
		return roleAds
	case match(cfg.LesrosesPass, roleLesroses):
		return roleLesroses
	}
	return ""
}

// basicAuthRole resolves credentials sent explicitly in the Authorization header.
func basicAuthRole(cfg config.Config, r *http.Request) string {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return ""
	}
	match := func(u, p string) bool {
		return u != "" && p != "" &&
			subtle.ConstantTimeCompare([]byte(user), []byte(u)) == 1 &&
			subtle.ConstantTimeCompare([]byte(pass), []byte(p)) == 1
	}
	switch {
	case match(cfg.AdminUser, cfg.AdminPass):
		return roleAdmin
	case match(cfg.AdsUser, cfg.AdsPass):
		return roleAds
	case match(cfg.LesrosesUser, cfg.LesrosesPass):
		return roleLesroses
	}
	return ""
}

// roleAllows reports whether a role may reach the path.
func roleAllows(role, p string) bool {
	switch role {
	case roleAdmin:
		return true
	case roleAds:
		return adsScoped(p)
	case roleLesroses:
		return lesrosesScoped(p)
	}
	return false
}

func isHTTPS(cfg config.Config, r *http.Request) bool {
	return r.TLS != nil ||
		r.Header.Get("X-Forwarded-Proto") == "https" ||
		strings.HasPrefix(cfg.PublicAppURL, "https://")
}

func setSession(cfg config.Config, w http.ResponseWriter, r *http.Request, secret, role string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionValue(secret, role),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(cfg, r),
		SameSite: http.SameSiteLaxMode,
	})
}

// safeNext keeps post-login redirects inside the admin area, so ?next= can't be
// turned into an open redirect.
func safeNext(raw, fallback string) string {
	if raw == "" || !strings.HasPrefix(raw, "/admin") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	return raw
}

func homeFor(role string) string {
	switch role {
	case roleAds:
		return adsPagePath
	case roleLesroses:
		return lesrosesPagePath
	}
	return "/admin/orders"
}

// redeemAdsToken exchanges ?k=<token> on the funnel page for a session and
// redirects to the same URL without the token, so it stops leaking through
// Referer headers and analytics page-view URLs. Reports whether it responded.
func redeemAdsToken(cfg config.Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.AdsAccessToken == "" || r.URL.Path != adsPagePath {
		return false
	}
	q := r.URL.Query()
	tok := q.Get(adsTokenParam)
	if tok == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(cfg.AdsAccessToken)) != 1 {
		return false
	}

	setSession(cfg, w, r, cfg.AdsAccessToken, roleAds)
	q.Del(adsTokenParam)
	target := adsPagePath
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return true
}

// loginLimiter throttles password guessing per client IP. In-memory is enough:
// a single process serves the whole site, and a restart resetting counters is
// not a meaningful weakening.
type loginLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{hits: map[string][]time.Time{}}
}

// blocked reports whether the IP has burned through its failed attempts. Only
// failures count (see fail/reset): several people behind one office NAT must not
// lock each other out just by logging in successfully.
func (l *loginLimiter) blocked(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, ip)
		return false
	}
	l.hits[ip] = kept
	return len(kept) >= loginMaxAttempts
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[ip] = append(l.hits[ip], time.Now())
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ip)
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="ru"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>Вход</title>
<style>
  *{box-sizing:border-box}
  body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
       padding:24px;background:#FDFBF7;color:#111;
       font:16px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif}
  form{width:100%;max-width:360px;background:#fff;border:1px solid #eee;border-radius:16px;
       padding:24px;box-shadow:0 4px 24px rgba(0,0,0,.06)}
  h1{margin:0 0 4px;font-size:20px}
  p.sub{margin:0 0 20px;color:#888;font-size:13px}
  label{display:block;margin-bottom:6px;font-size:12px;font-weight:700;
        text-transform:uppercase;letter-spacing:.04em;color:#888}
  input{width:100%;padding:12px 14px;margin-bottom:16px;border:1px solid #ddd;
        border-radius:10px;font-size:16px;background:#fff;color:#111}
  input:focus{outline:none;border-color:#D4AF37;box-shadow:0 0 0 3px rgba(212,175,55,.15)}
  button{width:100%;padding:14px;border:0;border-radius:10px;background:#D4AF37;color:#fff;
         font-size:16px;font-weight:700;cursor:pointer}
  button:active{transform:scale(.98)}
  .err{margin:0 0 16px;padding:10px 12px;border-radius:10px;background:#FEF2F2;
       color:#B91C1C;font-size:13px}
</style>
</head><body>
<form method="post" action="{{.LoginPath}}">
  <h1>Вход</h1>
  <p class="sub">Доступ к закрытому разделу</p>
  {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
  <input type="hidden" name="next" value="{{.Next}}">
  <label for="u">Логин</label>
  <input id="u" name="username" autocomplete="username" autocapitalize="none"
         autocorrect="off" spellcheck="false" required autofocus>
  <label for="p">Пароль</label>
  <input id="p" name="password" type="password" autocomplete="current-password" required>
  <button type="submit">Войти</button>
</form>
</body></html>`))

func renderLogin(w http.ResponseWriter, status int, next, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = loginTmpl.Execute(w, map[string]string{
		"LoginPath": loginPath,
		"Next":      next,
		"Error":     errMsg,
	})
}

// handleLogin serves the form and processes submissions.
func handleLogin(cfg config.Config, limiter *loginLimiter, w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"), "")

	if r.Method != http.MethodPost {
		if role := sessionRole(cfg, r); role != "" {
			http.Redirect(w, r, safeNext(next, homeFor(role)), http.StatusSeeOther)
			return
		}
		renderLogin(w, http.StatusOK, next, "")
		return
	}

	ip := clientIP(r)
	if limiter.blocked(ip) {
		renderLogin(w, http.StatusTooManyRequests, next, "Слишком много неудачных попыток. Попробуйте через 15 минут.")
		return
	}

	_ = r.ParseForm()
	next = safeNext(r.PostFormValue("next"), "")
	user := strings.TrimSpace(r.PostFormValue("username"))
	pass := r.PostFormValue("password")

	match := func(u, p string) bool {
		return u != "" && p != "" &&
			subtle.ConstantTimeCompare([]byte(user), []byte(u)) == 1 &&
			subtle.ConstantTimeCompare([]byte(pass), []byte(p)) == 1
	}
	switch {
	case match(cfg.AdminUser, cfg.AdminPass):
		limiter.reset(ip)
		setSession(cfg, w, r, cfg.AdminPass, roleAdmin)
		http.Redirect(w, r, safeNext(next, homeFor(roleAdmin)), http.StatusSeeOther)
	case match(cfg.AdsUser, cfg.AdsPass):
		limiter.reset(ip)
		setSession(cfg, w, r, cfg.AdsPass, roleAds)
		http.Redirect(w, r, safeNext(next, homeFor(roleAds)), http.StatusSeeOther)
	case match(cfg.LesrosesUser, cfg.LesrosesPass):
		limiter.reset(ip)
		setSession(cfg, w, r, cfg.LesrosesPass, roleLesroses)
		http.Redirect(w, r, safeNext(next, homeFor(roleLesroses)), http.StatusSeeOther)
	default:
		limiter.fail(ip)
		renderLogin(w, http.StatusUnauthorized, next, "Неверный логин или пароль.")
	}
}

func handleLogout(cfg config.Config, w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(cfg, r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, loginPath, http.StatusSeeOther)
}

// adminAuth gates /admin* and /api/admin/*. Pages send unauthenticated visitors
// to the login form; API paths answer 401 in JSON, because a redirect would just
// hand fetch() an HTML page. If ADMIN_USER / ADMIN_PASS are unset the admin area
// is disabled outright (503) so it can never be left wide open.
func adminAuth(cfg config.Config, next http.Handler) http.Handler {
	limiter := newLoginLimiter()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		isAPI := strings.HasPrefix(p, "/api/admin/")
		if !strings.HasPrefix(p, "/admin") && !isAPI {
			next.ServeHTTP(w, r)
			return
		}
		if cfg.AdminUser == "" || cfg.AdminPass == "" {
			http.Error(w, "admin disabled: set ADMIN_USER and ADMIN_PASS", http.StatusServiceUnavailable)
			return
		}
		switch p {
		case loginPath:
			handleLogin(cfg, limiter, w, r)
			return
		case logoutPath:
			handleLogout(cfg, w, r)
			return
		}
		if redeemAdsToken(cfg, w, r) {
			return
		}

		role := sessionRole(cfg, r)
		if role == "" {
			role = basicAuthRole(cfg, r)
		}
		if roleAllows(role, p) {
			// Роль — фронту: кабинет партнёра прячет навигацию по разделам admin.
			w.Header().Set("X-Admin-Role", role)
			next.ServeHTTP(w, r)
			return
		}

		if isAPI {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		if role != "" {
			// Вошёл, но раздел не для его роли — отправляем на свою домашнюю.
			http.Redirect(w, r, homeFor(role), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(p), http.StatusSeeOther)
	})
}
