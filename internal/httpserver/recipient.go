package httpserver

import "strings"

// Пол получателя песни. Не путать с полом вокала (relay.OrderPayload.VocalGender):
// песню женским голосом заказывают и для мужчины.
const (
	genderMale   = "male"
	genderFemale = "female"
)

// relationshipGender — пол получателя, однозначно заданный выбором «Кому дарим?»
// на первом шаге формы.
//
// «Друг», «Другое», «Партнёр» и любое незнакомое значение пола не задают: чипы
// настраиваются в админке, а «Друг» в этой форме выбирают и для подруги. Там
// решает явный тоггл «он/она» с формы, а если и его нет — промпт обходится без
// гендерно-маркированных форм. Гадать по имени нельзя: Саша, Женя, Никита и Илья
// в уменьшительной форме кончаются на -а/-я и при этом мужские.
var relationshipGender = map[string]string{
	"муж":     genderMale,
	"парень":  genderMale,
	"папа":    genderMale,
	"сын":     genderMale,
	"брат":    genderMale,
	"дедушка": genderMale,
	"жена":    genderFemale,
	"девушка": genderFemale,
	"мама":    genderFemale,
	"дочь":    genderFemale,
	"сестра":  genderFemale,
	"бабушка": genderFemale,
	"подруга": genderFemale,
	"любимая": genderFemale,
	"любимый": genderMale,
}

// recipientGender возвращает пол получателя песни: genderMale, genderFemale или
// пустую строку, если он неизвестен.
//
// Приоритет у явного выбора пользователя (recipientGender с формы) — он точнее
// любой карты. Вывод из отношения оставлен фолбэком: заказы приходят и от старых
// клиентов, и из мока, где поля может не быть.
func recipientGender(payload map[string]any) string {
	if g, _ := payload["recipientGender"].(string); g != "" {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case genderMale:
			return genderMale
		case genderFemale:
			return genderFemale
		}
	}
	if rel, _ := payload["relationship"].(string); rel != "" {
		return relationshipGender[strings.ToLower(strings.TrimSpace(rel))]
	}
	return ""
}
