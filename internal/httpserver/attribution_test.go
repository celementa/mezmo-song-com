package httpserver

import (
	"strings"
	"testing"
)

// Порядок приоритетов в attributionSource — то, от чего зависит колонка «Откуда»
// в админке и строки воронки в /admin/analytics.
func TestAttributionSource(t *testing.T) {
	const selfHost = "mezmo.ru"

	cases := []struct {
		name         string
		attr         map[string]any
		wantSource   string
		wantCampaign string
	}{
		{
			name:       "ref инфлюенсера",
			attr:       map[string]any{"ref": "vasya", "referrer": "https://instagram.com/"},
			wantSource: "ref:vasya",
		},
		{
			name:         "ref важнее utm_source",
			attr:         map[string]any{"ref": "vasya", "utm_source": "yandex", "utm_campaign": "713084347"},
			wantSource:   "ref:vasya",
			wantCampaign: "713084347",
		},
		{
			name:       "пустой ref не считается источником",
			attr:       map[string]any{"ref": "   ", "utm_source": "yandex"},
			wantSource: "yandex",
		},
		{
			// Обрезка по рунам, а не по байтам: ник может быть на кириллице.
			name:       "длинный ref обрезается до 64 символов",
			attr:       map[string]any{"ref": strings.Repeat("я", 200)},
			wantSource: "ref:" + strings.Repeat("я", 64),
		},
		{
			name:       "без меток — домен реферера",
			attr:       map[string]any{"referrer": "https://www.instagram.com/p/1"},
			wantSource: "instagram.com",
		},
		{
			name:       "свой домен в реферере — прямой заход",
			attr:       map[string]any{"referrer": "https://mezmo.ru/config"},
			wantSource: sourceDirect,
		},
		{
			name:       "пустая метка — прямой заход",
			attr:       map[string]any{},
			wantSource: sourceDirect,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, campaign := attributionSource(tc.attr, selfHost)
			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
			if campaign != tc.wantCampaign {
				t.Errorf("campaign = %q, want %q", campaign, tc.wantCampaign)
			}
		})
	}
}
