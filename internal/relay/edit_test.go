package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"text/template"
)

const editTestLyrics = "[Куплет 1]\nПервая строка\nВторая строка\n\n[Припев]\nПрипев как был"

// Промпт правки обязан содержать и текущий текст, и просьбу: без текста модель не
// знает, что сохранять, и переписывает песню целиком — ровно то поведение, из-за
// которого правки не работали.
func TestBuildEditPromptCarriesLyricsAndInstruction(t *testing.T) {
	const instruction = "замени вторую строку на «Новая строка»"
	got := buildEditPrompt("ru", editTestLyrics, instruction)

	if !strings.Contains(got, editTestLyrics) {
		t.Fatalf("промпт не содержит текущий текст песни:\n%s", got)
	}
	if !strings.Contains(got, instruction) {
		t.Fatalf("промпт не содержит просьбу пользователя:\n%s", got)
	}
}

// Файл шаблона живёт в data/prompts и правится на сервере без пересборки, поэтому
// его синтаксис нужно проверять отдельно: при ошибке разбора код молча уходит на
// встроенный фолбэк, и правка промпта на проде просто не применится.
func TestEditPromptFileParses(t *testing.T) {
	src, err := os.ReadFile("../../data/prompts/edit.md")
	if err != nil {
		t.Fatalf("read edit.md: %v", err)
	}
	tpl, err := template.New("edit").Parse(string(src))
	if err != nil {
		t.Fatalf("parse edit.md: %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, struct{ CurrentLyrics, Instruction string }{editTestLyrics, "просьба"}); err != nil {
		t.Fatalf("execute edit.md: %v", err)
	}
	if !strings.Contains(buf.String(), editTestLyrics) || !strings.Contains(buf.String(), "просьба") {
		t.Fatalf("edit.md не подставляет текст или просьбу:\n%s", buf.String())
	}
}

// Сквозная проверка правки: в OpenRouter уходит текущий текст, а результатом
// доработки становится ответ модели, а не сгенерированная с нуля песня.
func TestTriggerEditSendsCurrentLyricsAndReturnsEditedText(t *testing.T) {
	const edited = "[Куплет 1]\nПервая строка\nНовая строка\n\n[Припев]\nПрипев как был"

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &req); err != nil || len(req.Messages) == 0 {
			t.Errorf("некорректное тело запроса к OpenRouter: %s", raw)
			w.WriteHeader(500)
			return
		}
		gotPrompt = req.Messages[0].Content
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": edited}}},
		})
	}))
	t.Cleanup(srv.Close)

	orig := openRouterChatURL
	openRouterChatURL = srv.URL
	t.Cleanup(func() { openRouterChatURL = orig })

	// KieAPIKey пуст: постановка музыки в этом тесте не участвует.
	c := Client{OpenRouterAPIKey: "test-key"}
	got, err := c.TriggerEdit(context.Background(), OrderPayload{OrderID: "ord_e"}, editTestLyrics, "замени вторую строку")
	if err != nil {
		t.Fatalf("TriggerEdit err = %v", err)
	}
	if got != edited {
		t.Fatalf("текст = %q, want ответ модели %q", got, edited)
	}
	if !strings.Contains(gotPrompt, editTestLyrics) {
		t.Fatalf("в OpenRouter не ушёл текущий текст песни:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "замени вторую строку") {
		t.Fatalf("в OpenRouter не ушла просьба пользователя:\n%s", gotPrompt)
	}
}

// Английский заказ пишется по промптам data/prompts/en: текст песни и правка на
// английском, без русских пометок разделов.
func TestPromptsByLanguage(t *testing.T) {
	en := buildPrompt(OrderPayload{Recipient: "Emily", Language: "en"})
	if !strings.Contains(en, "English") || strings.Contains(en, "Куплет") {
		t.Fatalf("EN lyrics prompt is not English:\n%s", en)
	}
	ru := buildPrompt(OrderPayload{Recipient: "Аня"})
	if !strings.Contains(ru, "русск") {
		t.Fatalf("RU lyrics prompt lost Russian:\n%s", ru)
	}
	if edit := buildEditPrompt("en", "[Verse 1]\nline", "change it"); !strings.Contains(edit, "[Verse 1]") || strings.Contains(edit, "Текст песни") {
		t.Fatalf("EN edit prompt:\n%s", edit)
	}
}
