package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// kieBlockedWords — слова, которые Kie.ai трактует как имена артистов и блокирует генерацию.
var kieBlockedWords = []string{"skank", "skanking"}

var kieBlockedRe = func() *regexp.Regexp {
	escaped := make([]string, len(kieBlockedWords))
	for i, w := range kieBlockedWords {
		escaped[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(escaped, "|") + `)\b`)
}()

// vocalMentionRe — упоминание пола вокала в стайл-описании: «female vocals»,
// «male voice», «female singer».
var vocalMentionRe = regexp.MustCompile(`(?i)\b(?:fe)?male\s+(?:vocals?|voice|singer)\b`)

// styleWithVocal гарантирует, что выбранный пол вокала присутствует в стайл-строке,
// которая уходит в Kie.ai.
//
// Пол вокала едет туда двумя путями: параметром vocalGender и словами в описании
// стиля. Описание пишет модель по data/prompts/style.md, и если этот шаг упал или
// вернул пустое, style откатывается к жанру из заказа («Pop Ballad») — вокал из
// описания исчезает, и остаётся только параметр, который Suno соблюдает не всегда.
// Отсюда жалоба «выбрал женский голос, поёт мужской».
//
// Выбор пользователя главнее текста модели, поэтому чужое упоминание вокала
// заменяется, а не дополняется: «male vocals» в описании при заказанном женском
// голосе — прямое противоречие, и оставлять его в промпте нельзя.
func styleWithVocal(style, vocalGender string) string {
	tag := "female vocal"
	if vocalGender == "male" {
		tag = "male vocal"
	}

	style = strings.TrimSpace(vocalMentionRe.ReplaceAllString(style, tag))
	if style == "" {
		return tag
	}
	// После замены любое упоминание вокала в строке — уже наше, поэтому проверка
	// присутствия идёт тем же регулярным выражением. Подстрокой это не проверить:
	// «female vocal» содержит «male vocal» внутри себя.
	if vocalMentionRe.MatchString(style) {
		return style
	}
	return tag + ", " + style
}

// sanitizeKieStyle убирает из строки слова, блокируемые Kie.ai.
func sanitizeKieStyle(style string) string {
	cleaned := kieBlockedRe.ReplaceAllString(style, "offbeat")
	if cleaned != style {
		log.Printf("[relay] kie style sanitized: removed blocked words")
	}
	return cleaned
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

// proxiedClients кэширует HTTP-клиенты по URL прокси, чтобы не пересоздавать
// транспорт (и терять пул соединений) на каждый запрос.
var (
	proxiedMu      sync.Mutex
	proxiedClients = map[string]*http.Client{}
)

// openRouterClient возвращает клиент для запросов к OpenRouter: через прокси,
// если задан (OpenRouter геоблокирует российский IP прод-сервера), иначе прямой.
// Kie.ai ходит напрямую (httpClient) — у него IP-whitelist на прод-адрес.
func (c Client) openRouterClient() *http.Client {
	if c.OpenRouterProxy == "" {
		return httpClient
	}
	proxiedMu.Lock()
	defer proxiedMu.Unlock()
	if cl, ok := proxiedClients[c.OpenRouterProxy]; ok {
		return cl
	}
	u, err := url.Parse(c.OpenRouterProxy)
	if err != nil {
		log.Printf("[relay] invalid OPENROUTER_PROXY %q: %v — using direct", c.OpenRouterProxy, err)
		proxiedClients[c.OpenRouterProxy] = httpClient
		return httpClient
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(u)
	cl := &http.Client{Timeout: 120 * time.Second, Transport: tr}
	proxiedClients[c.OpenRouterProxy] = cl
	return cl
}

type Client struct {
	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenRouterProxy  string
	KieAPIKey        string
	CallbackURL      string // Kie.ai POSTs here when song is ready
}

type OrderPayload struct {
	OrderID   string
	Sender    string
	Recipient string
	Occasion  string
	Style     string
	Story     string
	// Emotion — главная эмоция песни («Любовь», «Благодарность», …): с v11
	// документа это основной выбор вместо жанра, жанр — необязательное уточнение.
	Emotion string
	// VocalGender — пол исполнителя (кто поёт), "male" или "female".
	VocalGender string
	// Relationship — кем получатель является автору («Жена», «Мама», «Друг»):
	// песня маме и песня жене должны быть разными по тону.
	Relationship string
	// RecipientGender — пол получателя песни, "male", "female" или пусто, если
	// неизвестен. Не совпадает с VocalGender: женским голосом поют и мужчине.
	// Пустое значение — сигнал промпту обойтись без гендерно-маркированных форм,
	// а не поставить род по умолчанию: угаданный неверно род это жалоба
	// «вместо „она“ поёт „он“».
	RecipientGender string
	// Language — язык заказа (ru | en): на нём пишутся текст песни и правки.
	Language string
}

// Trigger generates lyrics via OpenRouter, then queues music on Kie.ai.
// Returns generated lyrics (empty string if OpenRouterAPIKey not set).
func (c Client) Trigger(ctx context.Context, p OrderPayload) (string, error) {
	if c.OpenRouterAPIKey == "" {
		log.Printf("[relay] order=%s skip: no OpenRouter key", p.OrderID)
		return "", nil
	}

	log.Printf("[relay] order=%s step=lyrics model=%s", p.OrderID, c.OpenRouterModel)
	t0 := time.Now()
	lyrics, err := c.generateLyrics(ctx, p)
	if err != nil {
		log.Printf("[relay] order=%s step=lyrics err=%v elapsed=%s", p.OrderID, err, time.Since(t0))
		return "", fmt.Errorf("openrouter: %w", err)
	}
	log.Printf("[relay] order=%s step=lyrics ok len=%d elapsed=%s", p.OrderID, len(lyrics), time.Since(t0))

	return lyrics, c.queueMusic(ctx, p, lyrics)
}

// TriggerWithLyrics ставит генерацию музыки на уже готовом тексте, не обращаясь к
// OpenRouter за новым.
//
// Так работает доработка «свой текст»: пользователь прислал именно то, что хочет
// услышать, и переписывать здесь нечего. Раньше этот текст уходил в промпт как
// «история», модель сочиняла по нему свой — то есть вставить конкретный текст
// было физически невозможно, сколько бы раз человек ни пробовал.
//
// Возвращается тот же текст, что и пришёл: вызывающий сохраняет его как текст
// песни независимо от того, дошла ли очередь до Kie.ai.
func (c Client) TriggerWithLyrics(ctx context.Context, p OrderPayload, lyrics string) (string, error) {
	if strings.TrimSpace(lyrics) == "" {
		return "", fmt.Errorf("empty lyrics")
	}
	log.Printf("[relay] order=%s step=lyrics skipped: используем текст пользователя len=%d", p.OrderID, len(lyrics))
	return lyrics, c.queueMusic(ctx, p, lyrics)
}

// TriggerEdit правит уже готовый текст по просьбе пользователя вместо того, чтобы
// сочинять песню заново.
//
// Раньше просьба «поправь припев» дописывалась к истории заказа и запускала полную
// генерацию: текущий текст модель вообще не видела, поэтому менялась вся песня, а
// удачные строки терялись. Отсюда «делаю правки — всё снова переделывается».
//
// Пустая строка без ошибки означает, что ключа OpenRouter нет и править нечем —
// вызывающий трактует это так же, как и для Trigger.
func (c Client) TriggerEdit(ctx context.Context, p OrderPayload, currentLyrics, instruction string) (string, error) {
	if c.OpenRouterAPIKey == "" {
		log.Printf("[relay] order=%s skip: no OpenRouter key", p.OrderID)
		return "", nil
	}

	log.Printf("[relay] order=%s step=edit lyricsLen=%d", p.OrderID, len(currentLyrics))
	t0 := time.Now()
	lyrics, err := c.editLyrics(ctx, p, currentLyrics, instruction)
	if err != nil {
		log.Printf("[relay] order=%s step=edit err=%v elapsed=%s", p.OrderID, err, time.Since(t0))
		return "", fmt.Errorf("openrouter edit: %w", err)
	}
	log.Printf("[relay] order=%s step=edit ok len=%d elapsed=%s", p.OrderID, len(lyrics), time.Since(t0))

	return lyrics, c.queueMusic(ctx, p, lyrics)
}

// queueMusic подбирает стиль под текст и отправляет генерацию в Kie.ai.
// Стиль — отдельный запрос к OpenRouter: он описывает музыку под конкретный
// текст, поэтому нужен и для текста, присланного пользователем.
func (c Client) queueMusic(ctx context.Context, p OrderPayload, lyrics string) error {
	if c.KieAPIKey == "" {
		log.Printf("[relay] order=%s skip kie: no KIE_API_KEY", p.OrderID)
		return nil
	}

	style := p.Style
	if lyrics != "" && c.OpenRouterAPIKey != "" {
		log.Printf("[relay] order=%s step=style", p.OrderID)
		t1 := time.Now()
		if s, err := c.generateStyle(ctx, p, lyrics); err != nil {
			log.Printf("[relay] order=%s step=style err=%v elapsed=%s (using original style)", p.OrderID, err, time.Since(t1))
		} else if s != "" {
			log.Printf("[relay] order=%s step=style ok=%q elapsed=%s", p.OrderID, s, time.Since(t1))
			style = s
		} else {
			log.Printf("[relay] order=%s step=style empty response elapsed=%s (using original style)", p.OrderID, time.Since(t1))
		}
	}
	p.Style = style

	log.Printf("[relay] order=%s step=kie callbackURL=%s style=%q vocalGender=%q", p.OrderID, c.CallbackURL, p.Style, p.VocalGender)
	t2 := time.Now()
	if err := c.queueKie(ctx, p, lyrics); err != nil {
		log.Printf("[relay] order=%s step=kie err=%v elapsed=%s", p.OrderID, err, time.Since(t2))
		return fmt.Errorf("kie: %w", err)
	}
	log.Printf("[relay] order=%s step=kie queued ok elapsed=%s", p.OrderID, time.Since(t2))
	return nil
}

func (c Client) generateLyrics(ctx context.Context, p OrderPayload) (string, error) {
	content, err := c.chat(ctx, p.OrderID, "lyrics", buildPrompt(p))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("empty content in OpenRouter response")
	}
	return content, nil
}

// editLyrics правит готовый текст по просьбе пользователя, а не сочиняет новый.
func (c Client) editLyrics(ctx context.Context, p OrderPayload, currentLyrics, instruction string) (string, error) {
	content, err := c.chat(ctx, p.OrderID, "edit", buildEditPrompt(p.Language, currentLyrics, instruction))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("empty content in OpenRouter response")
	}
	return content, nil
}

func (c Client) generateStyle(ctx context.Context, p OrderPayload, lyrics string) (string, error) {
	return c.chat(ctx, p.OrderID, "style", buildStylePrompt(p, lyrics))
}

// openRouterChatURL — адрес completions-ручки OpenRouter. Переменная, а не
// константа, только ради тестов: подменить хост больше негде, HTTP-клиент
// собирается внутри пакета.
var openRouterChatURL = "https://openrouter.ai/api/v1/chat/completions"

// kieGenerateURL — постановка генерации музыки. Переменная по той же причине, что
// и openRouterChatURL: подменить хост в тестах больше негде.
var kieGenerateURL = "https://api.kie.ai/api/v1/generate"

// SetOpenRouterBaseURL перенаправляет запросы к модели на другой хост. Нужен
// локальному стенду: без ключа OpenRouter Trigger выходит рано и до Kie.ai
// дело не доходит вовсе — то есть без подмены базы конвейер «текст → музыка →
// трек в хранилище» локально не проверить ни разу.
func SetOpenRouterBaseURL(base string) {
	if base == "" {
		return
	}
	openRouterChatURL = strings.TrimSuffix(base, "/") + "/api/v1/chat/completions"
}

// SetKieBaseURL перенаправляет постановку генерации на другой хост. Нужен
// локальному стенду: настоящий Kie.ai требует публичный адрес для колбэка и
// белый список IP, поэтому воспроизвести колбэк локально можно только заглушкой.
// Пустая строка ничего не меняет.
func SetKieBaseURL(base string) {
	if base == "" {
		return
	}
	kieGenerateURL = strings.TrimSuffix(base, "/") + "/api/v1/generate"
}

// chat выполняет один запрос к OpenRouter и возвращает содержимое ответа как есть
// (пустое — тоже результат, решает вызывающий). Единая точка для всех трёх
// сценариев: генерация текста, подбор стиля и правка готового текста; label
// попадает в логи, чтобы шаг был виден грепом.
func (c Client) chat(ctx context.Context, orderID, label, prompt string) (string, error) {
	model := c.OpenRouterModel
	if model == "" {
		model = "openai/gpt-4o"
	}

	log.Printf("[relay] order=%s openrouter request step=%s model=%s promptLen=%d", orderID, label, model, len(prompt))

	reqBody := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	raw, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterChatURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.OpenRouterAPIKey)

	resp, err := c.openRouterClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	log.Printf("[relay] order=%s openrouter response step=%s status=%d content-type=%s", orderID, label, resp.StatusCode, resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[relay] order=%s openrouter decode err=%v step=%s body=%s", orderID, err, label, string(body))
		return "", err
	}
	if result.Error != nil {
		log.Printf("[relay] order=%s openrouter api error step=%s code=%d msg=%s", orderID, label, result.Error.Code, result.Error.Message)
		return "", fmt.Errorf("openrouter error %d: %s", result.Error.Code, result.Error.Message)
	}
	if len(result.Choices) == 0 {
		log.Printf("[relay] order=%s openrouter empty choices step=%s body=%s", orderID, label, string(body))
		return "", fmt.Errorf("empty choices in OpenRouter response")
	}
	return result.Choices[0].Message.Content, nil
}

func (c Client) queueKie(ctx context.Context, p OrderPayload, lyrics string) error {
	title := p.Recipient
	if title == "" {
		title = "Персональная песня"
		if p.Language == "en" {
			title = "Personal song"
		}
	}
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}

	vocalGender := "f"
	if p.VocalGender == "male" {
		vocalGender = "m"
	}

	style := styleWithVocal(sanitizeKieStyle(p.Style), p.VocalGender)

	reqBody := map[string]any{
		"prompt":       lyrics,
		"customMode":   true,
		"instrumental": false,
		"model":        "V5",
		"style":        style,
		"title":        title,
		"vocalGender":  vocalGender,
		"callBackUrl":  c.CallbackURL,
	}
	raw, _ := json.Marshal(reqBody)

	log.Printf("[relay] order=%s kie request body=%s", p.OrderID, string(raw))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kieGenerateURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.KieAPIKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[relay] order=%s kie response status=%d body=%s", p.OrderID, resp.StatusCode, string(body))

	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
