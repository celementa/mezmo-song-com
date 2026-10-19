package relay

import (
	"context"
	"testing"
)

// TriggerWithLyrics не имеет права менять присланный текст: это готовый результат,
// а не материал для генерации. Клиент без ключей в сеть не ходит, поэтому тест
// проверяет именно решение «не переписывать», а не поведение внешних API.
func TestTriggerWithLyricsDoesNotRewriteText(t *testing.T) {
	const text = "[Куплет 1]\nМой собственный текст\n"

	got, err := Client{}.TriggerWithLyrics(context.Background(), OrderPayload{OrderID: "ord_t"}, text)
	if err != nil {
		t.Fatalf("TriggerWithLyrics err = %v", err)
	}
	if got != text {
		t.Fatalf("текст = %q, want %q", got, text)
	}
}

func TestTriggerWithLyricsRejectsEmptyText(t *testing.T) {
	if _, err := (Client{}).TriggerWithLyrics(context.Background(), OrderPayload{OrderID: "ord_t"}, "   \n"); err == nil {
		t.Fatal("пустой текст принят, ожидалась ошибка")
	}
}
