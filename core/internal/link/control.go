package link

import "encoding/binary"

// Control-сообщения уровня link идут по control-каналу (frameControl). Это
// половина flow control «получатель → отправитель»: получатель сообщает
// отправителю, сколько своих объектов тот может держать неподтверждёнными на
// странице (окно приёма уровня link, rwnd_link).
//
// Адаптивный лимит параллельных записей не требует сообщений в протоколе — он
// выводится локально из ACK-RTT (см. limiter.go).
type ctrlType byte

const (
	ctrlWindowAdvertise ctrlType = 0
	ctrlLaneClose       ctrlType = 1
)

// encodeWindowAdvertise собирает тело WindowAdvertise: максимум объектов, которые
// отправитель может держать неподтверждёнными.
func encodeWindowAdvertise(maxObjects uint32) []byte {
	body := make([]byte, 1+4)
	body[0] = byte(ctrlWindowAdvertise)
	binary.BigEndian.PutUint32(body[1:], maxObjects)
	return body
}

func encodeLaneClose() []byte { return []byte{byte(ctrlLaneClose)} }

// parseControl декодирует тело control-сообщения (байты после байта kind).
func parseControl(body []byte) (ctrlType, uint32, bool) {
	if len(body) == 0 {
		return 0, 0, false
	}
	switch ctrlType(body[0]) {
	case ctrlWindowAdvertise:
		if len(body) < 1+4 {
			return 0, 0, false
		}
		return ctrlWindowAdvertise, binary.BigEndian.Uint32(body[1:5]), true
	case ctrlLaneClose:
		return ctrlLaneClose, 0, len(body) == 1
	default:
		return 0, 0, false
	}
}
