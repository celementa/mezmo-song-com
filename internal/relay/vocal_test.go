package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Пол вокала обязан присутствовать в стайл-строке, а не только в параметре
// vocalGender: описание стиля пишет модель, и когда её шаг падает, style
// откатывается к жанру из заказа — вокал из промпта пропадает совсем.
func TestStyleWithVocal(t *testing.T) {
	cases := []struct {
		name        string
		style       string
		vocalGender string
		want        string
	}{
		{
			name:        "в описании вокала нет — тег добавляется",
			style:       "Warm acoustic pop ballad, soft guitar, moderate tempo.",
			vocalGender: "female",
			want:        "female vocal, Warm acoustic pop ballad, soft guitar, moderate tempo.",
		},
		{
			name:        "жанр из заказа после сбоя генерации стиля",
			style:       "Pop Ballad",
			vocalGender: "male",
			want:        "male vocal, Pop Ballad",
		},
		{
			name:        "пустой стиль",
			style:       "",
			vocalGender: "female",
			want:        "female vocal",
		},
		{
			name:        "модель указала противоположный пол — выбор пользователя главнее",
			style:       "Emotional ballad with male vocals and piano.",
			vocalGender: "female",
			want:        "Emotional ballad with female vocal and piano.",
		},
		{
			name:        "нужный пол уже указан — не дублируем",
			style:       "Dreamy synthpop, female voice, wide reverb.",
			vocalGender: "female",
			want:        "Dreamy synthpop, female vocal, wide reverb.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := styleWithVocal(tc.style, tc.vocalGender)
			if got != tc.want {
				t.Fatalf("styleWithVocal(%q, %q)\n got: %q\nwant: %q", tc.style, tc.vocalGender, got, tc.want)
			}
			// Противоположного пола в итоге быть не должно ни при каком входе.
			// Проверка по границе слова, а не подстрокой: «female vocal» содержит
			// «male vocal» внутри себя.
			opposite := regexp.MustCompile(`(?i)\bmale vocal`)
			if tc.vocalGender == "male" {
				opposite = regexp.MustCompile(`(?i)\bfemale vocal`)
			}
			if opposite.MatchString(got) {
				t.Fatalf("в стиле остался противоположный пол вокала: %q", got)
			}
		})
	}
}

// Сквозная проверка: в запрос к Kie.ai уходит и параметр vocalGender, и тег в
// стайл-строке — два независимых канала, потому что параметр Suno соблюдает не
// железно.
func TestQueueKieSendsVocalInBothPlaces(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("некорректное тело запроса к Kie.ai: %s", raw)
		}
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	t.Cleanup(srv.Close)

	orig := kieGenerateURL
	kieGenerateURL = srv.URL
	t.Cleanup(func() { kieGenerateURL = orig })

	c := Client{KieAPIKey: "test-key"}
	p := OrderPayload{OrderID: "ord_v", Recipient: "Ира", Style: "Pop Ballad", VocalGender: "female"}
	if err := c.queueKie(context.Background(), p, "[Куплет 1]\nтекст"); err != nil {
		t.Fatalf("queueKie err = %v", err)
	}

	if body["vocalGender"] != "f" {
		t.Fatalf("vocalGender = %v, want f", body["vocalGender"])
	}
	style, _ := body["style"].(string)
	if !strings.Contains(strings.ToLower(style), "female vocal") {
		t.Fatalf("style = %q, want упоминание female vocal", style)
	}
}
