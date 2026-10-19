package httpserver

import "testing"

// Пол получателя — причина жалобы «вместо слов „она" делает слово „он"». Явный
// выбор пользователя всегда сильнее вывода из отношения, а неизвестный пол
// обязан оставаться неизвестным: угаданный неверно род и есть та самая жалоба.
func TestRecipientGender(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"явный выбор с формы", map[string]any{"recipientGender": "female"}, genderFemale},
		{"явный выбор сильнее отношения", map[string]any{"relationship": "Муж", "recipientGender": "female"}, genderFemale},
		{"из отношения: жена", map[string]any{"relationship": "Жена"}, genderFemale},
		{"из отношения: муж", map[string]any{"relationship": "Муж"}, genderMale},
		{"из отношения: мама", map[string]any{"relationship": "Мама"}, genderFemale},
		{"регистр и пробелы не мешают", map[string]any{"relationship": "  ДЕВУШКА "}, genderFemale},
		{"друг — пол не определён словом", map[string]any{"relationship": "Друг"}, ""},
		{"другое — свободный ввод", map[string]any{"relationship": "Другое"}, ""},
		{"партнёр — дефолт формы, пол не задан", map[string]any{"relationship": "Партнёр"}, ""},
		{"незнакомый чип из админки", map[string]any{"relationship": "Коллега"}, ""},
		{"мусор в явном поле игнорируется", map[string]any{"recipientGender": "мужик"}, ""},
		{"мусор в явном поле не отменяет отношение", map[string]any{"relationship": "Жена", "recipientGender": "нет"}, genderFemale},
		{"пустой заказ", map[string]any{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recipientGender(tc.payload); got != tc.want {
				t.Fatalf("recipientGender(%v) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}
