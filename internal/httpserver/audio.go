package httpserver

import (
	"fmt"
	"strconv"
)

// Ключ трека в MinIO и публичная ссылка на него привязаны к номеру прогона
// генерации (orders.regeneration_count).
//
// Раньше ключ был `orders/{id}/track-{i}.mp3`, а ссылка — `/audio/{i}`: на каждой
// доработке объект в MinIO перезаписывался, но URL оставался тем же, и браузер
// час играл прошлую песню из своего кэша (Cache-Control ниже). Со стороны клиента
// это выглядело как «текст исправился, а поётся старая песня» — самая частая
// жалоба на доработки.
//
// Номер прогона в ключе делает каждый прогон отдельным объектом, а он же в
// query-параметре ссылки — отдельным URL, который кэшу подменить нечем.

// audioObjectKey возвращает ключ трека в MinIO. trackNumber — номер трека в
// прогоне, начиная с 1 (Kie.ai обычно присылает два варианта на запрос).
func audioObjectKey(orderID string, regeneration, trackNumber int) string {
	return fmt.Sprintf("orders/%s/r%d/track-%d.mp3", orderID, regeneration, trackNumber)
}

// publicAudioURL возвращает ссылку на трек для фронта. trackIndex — индекс в
// orders.audio_urls (с нуля), по нему трек и находит handleAudioProxy; параметр
// `v` на разбор ссылки не влияет и нужен только чтобы ссылка менялась вместе с
// содержимым.
func publicAudioURL(ref string, trackIndex, regeneration int) string {
	return "/api/public/order/" + ref + "/audio/" + strconv.Itoa(trackIndex) +
		"?v=" + strconv.Itoa(regeneration)
}
