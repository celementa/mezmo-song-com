package relay

import (
	"os"
	"strings"
	"testing"
	"text/template"
)

// Промпт генерации обязан явно называть род получателя и отношение к нему.
// Проверяется на реальном файле data/prompts/lyrics.md — он правится на сервере
// без пересборки, и при ошибке разбора код молча уходит на встроенный фолбэк.
func TestLyricsPromptFileStatesRecipientGender(t *testing.T) {
	src, err := os.ReadFile("../../data/prompts/lyrics.md")
	if err != nil {
		t.Fatalf("read lyrics.md: %v", err)
	}
	tpl, err := template.New("lyrics").Parse(string(src))
	if err != nil {
		t.Fatalf("parse lyrics.md: %v", err)
	}

	render := func(gender string) string {
		t.Helper()
		var buf strings.Builder
		data := struct {
			Recipient, Sender, Occasion, Style, Emotion, Story, VocalGender, Relationship, RecipientGender string
		}{
			Recipient:       "Ната́ша",
			VocalGender:     "male",
			Relationship:    "Жена",
			RecipientGender: gender,
		}
		if err := tpl.Execute(&buf, data); err != nil {
			t.Fatalf("execute lyrics.md (gender=%q): %v", gender, err)
		}
		return buf.String()
	}

	female := render("female")
	if !strings.Contains(female, "Получатель песни — женщина") {
		t.Fatalf("промпт не называет получателя женщиной:\n%s", female)
	}
	if strings.Contains(female, "Получатель песни — мужчина") {
		t.Fatalf("в промпте одновременно оба рода получателя:\n%s", female)
	}
	if !strings.Contains(female, "Кем получатель является автору: Жена") {
		t.Fatalf("промпт не передаёт отношение к получателю:\n%s", female)
	}

	male := render("male")
	if !strings.Contains(male, "Получатель песни — мужчина") {
		t.Fatalf("промпт не называет получателя мужчиной:\n%s", male)
	}

	// Неизвестный пол не должен превращаться в род по умолчанию.
	unknown := render("")
	if strings.Contains(unknown, "Получатель песни — женщина") || strings.Contains(unknown, "Получатель песни — мужчина") {
		t.Fatalf("при неизвестном поле промпт всё равно назначает род:\n%s", unknown)
	}
	if !strings.Contains(unknown, "Пол получателя неизвестен") {
		t.Fatalf("промпт не просит обойтись без гендерных форм:\n%s", unknown)
	}
}

// Тот же контракт для встроенного фолбэка: если файл промпта недоступен, род
// получателя всё равно должен быть указан.
func TestBuildPromptFallbackStatesRecipientGender(t *testing.T) {
	// В тестах cwd — internal/relay, файла data/prompts/lyrics.md по этому пути
	// нет, поэтому buildPrompt работает на defaultLyricsPrompt.
	got := buildPrompt(OrderPayload{Recipient: "Ира", Relationship: "Мама", RecipientGender: genderFemaleValue})
	if !strings.Contains(got, "Получатель — женщина") {
		t.Fatalf("фолбэк-промпт не называет род получателя:\n%s", got)
	}
	if !strings.Contains(got, "Кем получатель является автору: Мама") {
		t.Fatalf("фолбэк-промпт не передаёт отношение:\n%s", got)
	}
}

// genderFemaleValue — то же значение, что кладёт в payload httpserver; дублируется
// строкой, чтобы пакет relay не зависел от httpserver.
const genderFemaleValue = "female"
