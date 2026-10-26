package httpserver

import (
	"regexp"
	"strings"
)

// Пол вокала, попрошенный словами в тексте правки («сделайте мужской голос»,
// «пусть поёт мужчина», "male voice"). Дропдаун «Вокал» — не единственный способ
// попросить другой голос: люди пишут это в поле «Опишите правки». До этой
// эвристики такая просьба доезжала только до текста песни (он переписывался
// в нужном роде), а петь продолжал прежний голос — жалоба «просил мужской,
// выдаёт женский».
var (
	// Слово из вокального контекста — без него упоминание пола («песня для мужа»)
	// не считается просьбой сменить голос.
	vocalContextRe = regexp.MustCompile(`(?i)голос|вокал|по[её]т|пел|спо[йе]|исполн|voice|vocal|sing`)
	// Упоминание пола. female раньше male, иначе male матчится внутри female.
	vocalGenderMentionRe = regexp.MustCompile(`(?i)женск\p{L}*|женщин\p{L}*|мужск\p{L}*|мужчин\p{L}*|female|male`)
)

// vocalGenderFromText возвращает "male"/"female", если текст правки явно просит
// пол вокала, иначе "". При нескольких упоминаниях побеждает последнее:
// в «замените женский голос на мужской» цель — последняя.
func vocalGenderFromText(text string) string {
	if !vocalContextRe.MatchString(text) {
		return ""
	}
	m := vocalGenderMentionRe.FindAllString(text, -1)
	if len(m) == 0 {
		return ""
	}
	last := strings.ToLower(m[len(m)-1])
	if strings.HasPrefix(last, "муж") || last == "male" {
		return "male"
	}
	return "female"
}

// vocalGenderFor — пол исполнителя для генерации. Явный выбор голоса
// (musicPreferences.voiceStyle: male/female) побеждает. Выбора в форме больше
// нет (v11 документа), поэтому без него пол выводится из адресата: песню
// поют ОТ автора К получателю, и для пары исполнитель — противоположного пола
// (жене поёт мужчина, мужу — женщина). Для мамы, папы, друга и «другого»
// пол автора неизвестен — остаётся прежний дефолт, женский вокал.
func vocalGenderFor(payload map[string]any, voiceStyle string) string {
	vs := strings.ToLower(strings.TrimSpace(voiceStyle))
	switch {
	case vs == "male" || strings.HasPrefix(vs, "male") || strings.Contains(vs, "муж"):
		return "male"
	case vs == "female" || strings.HasPrefix(vs, "female") || strings.Contains(vs, "жен"):
		return "female"
	}
	switch recipientGender(payload) {
	case genderFemale:
		if rel, _ := payload["relationship"].(string); isPartner(rel) {
			return "male"
		}
	case genderMale:
		if rel, _ := payload["relationship"].(string); isPartner(rel) {
			return "female"
		}
	}
	return "female"
}

// isPartner — отношение, в котором автор и получатель пара: только здесь пол
// автора выводится из пола получателя.
func isPartner(relationship string) bool {
	switch strings.ToLower(strings.TrimSpace(relationship)) {
	case "муж", "жена", "парень", "девушка", "любимый", "любимая",
		"husband", "wife", "boyfriend", "girlfriend":
		return true
	}
	return false
}
