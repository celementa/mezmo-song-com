package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port          string
	DatabasePath  string
	LogDir        string // каталог файловых логов; пусто — только stdout
	LogKeepDays   int
	PublicAppURL  string
	RelayURL      string
	RelaySecret   string
	WebhookSecret string

	AdminUser string
	AdminPass string
	// AdsUser/AdsPass — ограниченная учётка для модераторов рекламных площадок:
	// открывает только /admin/ads, но не заказы и не аналитику (см. adminAuth).
	AdsUser string
	AdsPass string
	// AdsAccessToken opens /admin/ads by link, without typing credentials.
	AdsAccessToken string
	// LesrosesUser/LesrosesPass — кабинет партнёра Les Roses: только
	// /admin/lesroses и его API (см. adminAuth и lesroses.go).
	LesrosesUser string
	LesrosesPass string

	// InSales магазина Les Roses: API-ключ (идентификатор + пароль) для
	// перепроверки заказов из вебхука. Пусто — интеграция выключена.
	InsalesShopURL     string
	InsalesAPIID       string
	InsalesAPIPassword string
	// LesrosesWebhookToken — секрет в URL вебхука InSales (подписи у их
	// вебхуков нет; тело всё равно перепроверяется через API).
	LesrosesWebhookToken string
	// LesrosesOrigins — сайты, с которых сниппет корзины шлёт события показов.
	LesrosesOrigins []string

	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenRouterProxy  string
	KieAPIKey        string
	// Базы внешних API. Пусто — боевые адреса из самих пакетов. Задаются только
	// локальным стендом (scripts/stand-stubs.mjs), чтобы платёжный конвейер и
	// колбэк генерации проходились целиком без выхода в интернет.
	YooKassaAPIBase   string
	KieAPIBase        string
	OpenRouterAPIBase string
	// AudioAllowPrivate — послабление SSRF-защиты загрузчика для локального
	// стенда (см. storage.AllowPrivateAudioTargets). На проде не задаётся.
	AudioAllowPrivate bool

	YooKassaShopID    string
	YooKassaSecretKey string
	YooKassaVatCode   int

	// UnitPay — приём оплаты mezmo-song.com (internal/payment/unitpay.go).
	UnitPayProjectID string
	UnitPaySecretKey string
	// UnitPayTest — тестовый режим API: только RUB, поэтому сумма в валюте сайта
	// пересчитывается по UnitPayTestRubRate (тот же курс, что RUB_PER_USD на фронте).
	UnitPayTest        bool
	UnitPayLogin       string
	UnitPayCurrency    string  // валюта цен сайта и платежа, ISO 4217
	UnitPayTestRubRate float64 // ₽ за единицу валюты сайта — только для тестового режима
	UnitPayAPIBase     string  // только для стенда/тестов
	// UnitPaySkipIPCheck — принимать уведомления с любого адреса. Только для
	// стенда: на проде без проверки IP подпись остаётся единственной защитой.
	UnitPaySkipIPCheck bool

	// Unisender (email-маркетинг): без ключа события не уходят.
	UnisenderAPIKey  string
	UnisenderListID  string
	UnisenderAPIBase string // только для стенда/тестов
	// URL блока «API-триггер» сценария Unisender: события по каждому заказу
	// (order_created / order_paid / song_ready / …). Пусто — не отправляются.
	UnisenderTriggerURL string

	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string // имя в поле From, по умолчанию Mezmo
	MailReplyTo  string
	SMTPNoTLS    bool
	// SMTP смотрит в Unisender Go: письмо получает заголовок X-UNISENDER-GO
	// (без трекинга ссылок) и свою ссылку отписки {{UnsubscribeUrl}} в
	// подписи — тогда Go не добавляет стандартный блок отписки.
	SMTPUnisenderGo bool

	// StaticDir — собранный фронт (frontend/dist): из него Go отдаёт страницы
	// админки после авторизации. Публичный сайт из той же папки отдаёт nginx.
	StaticDir string
	// AudioDir — каталог, где лежат скачанные треки и отрывки (internal/storage).
	AudioDir string
	// AudioAllowedHosts — суффиксы доменов, откуда разрешено качать аудио.
	// Значение на проде: AUDIO_ALLOWED_HOSTS="aiquickdraw.com,files.freemusicarchive.org".
	// Именно aiquickdraw.com, а не kie.ai: Kie.ai отдаёт готовые треки со своего
	// CDN (tempfile.aiquickdraw.com), и allowlist с "kie.ai" заблокировал бы
	// загрузку всех песен. Пусто — любой публичный хост; приватные адреса
	// запрещены всегда.
	AudioAllowedHosts []string

	GA4MeasurementID string
	GA4APISecret     string

	// Серверные конверсии Метрики (офлайн-конверсии) и VK Ads (s2s-трекер).
	// Синк молчит, пока не заполнены его переменные.
	MetrikaCounterID  string
	MetrikaOAuthToken string
	VKPixelID         string
	VKGoal            string
}

func Load() Config {
	return Config{
		Port:                 getenv("PORT", "8080"),
		DatabasePath:         getenv("DATABASE_PATH", "./data/ai-songs.db"),
		LogDir:               getenv("LOG_DIR", filepath.Join(filepath.Dir(getenv("DATABASE_PATH", "./data/ai-songs.db")), "logs")),
		LogKeepDays:          getenvInt("LOG_KEEP_DAYS", 30),
		PublicAppURL:         getenv("PUBLIC_APP_URL", "http://127.0.0.1:8080"),
		RelayURL:             os.Getenv("RELAY_URL"),
		RelaySecret:          os.Getenv("RELAY_SECRET"),
		WebhookSecret:        getenv("WEBHOOK_SECRET", "dev-change-me"),
		AdminUser:            os.Getenv("ADMIN_USER"),
		AdminPass:            os.Getenv("ADMIN_PASS"),
		AdsUser:              os.Getenv("ADS_USER"),
		AdsPass:              os.Getenv("ADS_PASS"),
		AdsAccessToken:       os.Getenv("ADS_ACCESS_TOKEN"),
		LesrosesUser:         os.Getenv("LESROSES_USER"),
		LesrosesPass:         os.Getenv("LESROSES_PASS"),
		InsalesShopURL:       strings.TrimSuffix(os.Getenv("INSALES_SHOP_URL"), "/"),
		InsalesAPIID:         os.Getenv("INSALES_API_ID"),
		InsalesAPIPassword:   os.Getenv("INSALES_API_PASSWORD"),
		LesrosesWebhookToken: os.Getenv("LESROSES_WEBHOOK_TOKEN"),
		LesrosesOrigins:      splitCSV(getenv("LESROSES_ORIGINS", "https://lesrosesdechantal.ru,https://www.lesrosesdechantal.ru")),
		OpenRouterAPIKey:     os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:      getenv("OPENROUTER_MODEL", "openai/gpt-4o"),
		OpenRouterProxy:      os.Getenv("OPENROUTER_PROXY"),
		KieAPIKey:            os.Getenv("KIE_API_KEY"),
		YooKassaAPIBase:      os.Getenv("YOOKASSA_API_BASE"),
		KieAPIBase:           os.Getenv("KIE_API_BASE"),
		OpenRouterAPIBase:    os.Getenv("OPENROUTER_API_BASE"),
		AudioAllowPrivate:    os.Getenv("AUDIO_ALLOW_PRIVATE") == "1",
		YooKassaShopID:       os.Getenv("YOOKASSA_SHOP_ID"),
		YooKassaSecretKey:    os.Getenv("YOOKASSA_SECRET_KEY"),
		YooKassaVatCode:      getenvInt("YOOKASSA_VAT_CODE", 1),
		UnitPayProjectID:     os.Getenv("UNITPAY_PROJECT_ID"),
		UnitPaySecretKey:     os.Getenv("UNITPAY_SECRET_KEY"),
		UnitPayTest:          os.Getenv("UNITPAY_TEST") == "1",
		UnitPayLogin:         os.Getenv("UNITPAY_LOGIN"),
		UnitPayCurrency:      getenv("UNITPAY_CURRENCY", "USD"),
		UnitPayTestRubRate:   getenvFloat("UNITPAY_TEST_RUB_RATE", 85.5),
		UnitPayAPIBase:       os.Getenv("UNITPAY_API_BASE"),
		UnitPaySkipIPCheck:   os.Getenv("UNITPAY_SKIP_IP_CHECK") == "1",
		UnisenderAPIKey:      os.Getenv("UNISENDER_API_KEY"),
		UnisenderListID:      getenv("UNISENDER_LIST_ID", "1"),
		UnisenderAPIBase:     os.Getenv("UNISENDER_API_BASE"),
		UnisenderTriggerURL:  os.Getenv("UNISENDER_TRIGGER_URL"),
		SMTPHost:             os.Getenv("SMTP_HOST"),
		SMTPPort:             getenv("SMTP_PORT", "587"),
		SMTPUser:             os.Getenv("SMTP_USER"),
		SMTPPassword:         os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:             os.Getenv("SMTP_FROM"),
		SMTPFromName:         getenv("SMTP_FROM_NAME", "Mezmo"),
		MailReplyTo:          os.Getenv("MAIL_REPLY_TO"),
		SMTPNoTLS:            os.Getenv("SMTP_INSECURE") == "true",
		SMTPUnisenderGo:      os.Getenv("SMTP_UNISENDER_GO") == "true",
		StaticDir:            getenv("STATIC_DIR", "./frontend/dist"),
		AudioDir:             getenv("AUDIO_DIR", filepath.Join(filepath.Dir(getenv("DATABASE_PATH", "./data/ai-songs.db")), "audio")),
		AudioAllowedHosts:    splitCSV(os.Getenv("AUDIO_ALLOWED_HOSTS")),
		GA4MeasurementID:     os.Getenv("GA4_MEASUREMENT_ID"),
		GA4APISecret:         os.Getenv("GA4_API_SECRET"),
		MetrikaCounterID:     os.Getenv("METRIKA_COUNTER_ID"),
		MetrikaOAuthToken:    os.Getenv("METRIKA_OAUTH_TOKEN"),
		VKPixelID:            os.Getenv("VK_PIXEL_ID"),
		VKGoal:               getenv("VK_GOAL", "purchase"),
	}
}

// splitCSV parses a comma-separated env value into a trimmed, non-empty slice.
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func getenvInt(k string, def int) int {
	return AtoiDef(os.Getenv(k), def)
}

func getenvFloat(k string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(k)), 64)
	if err != nil || f <= 0 {
		return def
	}
	return f
}

func AtoiDef(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
