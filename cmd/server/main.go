package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mezmo-song-com/internal/config"
	"mezmo-song-com/internal/httpserver"
	"mezmo-song-com/internal/logfile"
	"mezmo-song-com/internal/payment"
	"mezmo-song-com/internal/relay"
	"mezmo-song-com/internal/storage"
	"mezmo-song-com/internal/store"

	"github.com/joho/godotenv"
)

func main() {
	// Load, а не Overload: переменные, уже выставленные окружением, побеждают
	// файл. Иначе .env (на машине разработчика — с БОЕВЫМИ ключами) перебивал
	// настройки локального стенда, и стенд либо не поднимался, либо ходил в
	// живые ЮKassa/SMTP/OpenRouter. DOTENV_SKIP=1 выключает чтение файла
	// целиком — так стенд гарантирует, что из .env не подтянется ничего.
	if os.Getenv("DOTENV_SKIP") != "1" {
		_ = godotenv.Load(".env")
	}

	cfg := config.Load()
	// Файловые логи на томе данных: `docker logs` обнуляются каждым деплоем, а
	// разбор потерянного вебхука ЮKassa нужен и через неделю.
	if cfg.LogDir != "" {
		if lw, err := logfile.New(cfg.LogDir, cfg.LogKeepDays); err != nil {
			log.Printf("logfile: %s недоступен (%v) — пишем только в stdout", cfg.LogDir, err)
		} else {
			log.SetOutput(io.MultiWriter(os.Stdout, lw))
			log.Printf("logfile: %s (храним %d дн.)", lw.Path(), cfg.LogKeepDays)
		}
	}
	// Базы внешних API подменяет только стенд (см. scripts/stand.sh full).
	if cfg.UnitPayAPIBase != "" {
		payment.UnitPayAPIBase = cfg.UnitPayAPIBase
		log.Printf("unitpay: API base overridden → %s", cfg.UnitPayAPIBase)
	}
	if cfg.YooKassaAPIBase != "" {
		payment.APIBaseURL = cfg.YooKassaAPIBase
		log.Printf("yookassa: API base overridden → %s", cfg.YooKassaAPIBase)
	}
	if cfg.KieAPIBase != "" {
		relay.SetKieBaseURL(cfg.KieAPIBase)
		log.Printf("kie: API base overridden → %s", cfg.KieAPIBase)
	}
	if cfg.OpenRouterAPIBase != "" {
		relay.SetOpenRouterBaseURL(cfg.OpenRouterAPIBase)
		log.Printf("openrouter: API base overridden → %s", cfg.OpenRouterAPIBase)
	}
	if cfg.AudioAllowPrivate {
		storage.AllowPrivateAudioTargets = true
		log.Printf("ВНИМАНИЕ: AUDIO_ALLOW_PRIVATE=1 — защита загрузчика от SSRF выключена (режим стенда)")
	}
	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	sc, err := storage.New(cfg.AudioDir, cfg.AudioAllowedHosts)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	log.Printf("storage: %s", cfg.AudioDir)

	mux := http.NewServeMux()
	srv := &httpserver.Server{DB: db, Config: cfg, Storage: sc}
	srv.Register(mux)
	// Подписки: напоминания, ежемесячные автосписания и закрытие просроченных.
	srv.StartSubscriptionJobs(context.Background())
	// Джобы A/B-флоу: досылка письма после снятия задержки доставки.
	srv.StartFlowJobs(context.Background())
	// Сверка оплат с провайдером: подбирает платежи, уведомление о которых не дошло.
	srv.StartPaymentReconciliation(context.Background())
	// Les Roses: сверка заказов InSales и напоминания заполнить анкету.
	srv.StartLesrosesJobs(context.Background())
	// Публичный сайт отдаёт nginx из STATIC_DIR (туда же выкладывает CI фронта);
	// сюда nginx проксирует только /api и /admin. Страницы админки Go отдаёт
	// сам — после adminAuth, — из той же папки, поэтому отдельной сборки и
	// вшитого в бинарь фронта нет.
	mux.Handle("/", spa(os.DirFS(cfg.StaticDir)))

	// relay
	if cfg.OpenRouterAPIKey != "" {
		log.Printf("openrouter: model=%s", cfg.OpenRouterModel)
	} else {
		log.Printf("openrouter: disabled (no API key)")
	}
	if cfg.KieAPIKey != "" {
		log.Printf("kie.ai: enabled")
	} else {
		log.Printf("kie.ai: disabled (no API key)")
	}

	// payment
	switch {
	case cfg.UnitPayProjectID != "":
		log.Printf("unitpay: project=%s currency=%s test=%v", cfg.UnitPayProjectID, cfg.UnitPayCurrency, cfg.UnitPayTest)
	case cfg.YooKassaShopID != "":
		log.Printf("yookassa: shop=%s", cfg.YooKassaShopID)
	default:
		log.Printf("payments: mock mode (no credentials)")
	}

	// smtp
	if cfg.SMTPHost != "" {
		log.Printf("smtp: %s:%s from=%s", cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)
	} else {
		log.Printf("smtp: disabled")
	}

	addr := ":" + cfg.Port
	log.Printf("ai-songs listening on %s  db=%s  url=%s", addr, cfg.DatabasePath, cfg.PublicAppURL)
	log.Fatal(http.ListenAndServe(addr, adminAuth(cfg, mux)))
}

// spa отдаёт файлы фронта из content и index.html на любой другой путь.
// index.html читается на каждый запрос: выкладка фронта меняет имена чанков,
// и закэшированный при старте index ссылался бы на удалённые файлы.
func spa(content fs.FS) http.Handler {
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
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(indexHTML))
	})
}
