package link

import "encoding/binary"

// Кадр link начинается с байта kind, разделяющего payload верхнего уровня и
// собственный control-канал канала:
//
//	kind = frameData    : [1] [seq:8] [payload]   payload — кадр верхнего уровня
//	kind = frameControl : [1] [ctrlType:1] [body] служебка уровня link (см. control.go)
//
// Порядковый номер несут только data-кадры; control-кадры применяются в порядке
// прихода (носитель упорядочен по направлению, поэтому побеждает последнее
// объявление) и не участвуют в переупорядочивании.
type frameKind byte

const (
	frameData    frameKind = 0
	frameControl frameKind = 1
)

const dataHeaderLen = 1 + 8 // kind + seq

func encodeData(seq uint64, payload []byte) []byte {
	buf := make([]byte, dataHeaderLen+len(payload))
	buf[0] = byte(frameData)
	binary.BigEndian.PutUint64(buf[1:dataHeaderLen], seq)
	copy(buf[dataHeaderLen:], payload)
	return buf
}

func encodeControl(body []byte) []byte {
	buf := make([]byte, 1+len(body))
	buf[0] = byte(frameControl)
	copy(buf[1:], body)
	return buf
}

// decodeData возвращает seq и payload data-кадра. ok == false, если b не является
// корректным data-кадром.
func decodeData(b []byte) (seq uint64, payload []byte, ok bool) {
	if len(b) < dataHeaderLen || frameKind(b[0]) != frameData {
		return 0, nil, false
	}
	return binary.BigEndian.Uint64(b[1:dataHeaderLen]), b[dataHeaderLen:], true
}

// kindOf возвращает вид кадра и байты после байта kind.
func kindOf(b []byte) (frameKind, []byte, bool) {
	if len(b) == 0 {
		return 0, nil, false
	}
	return frameKind(b[0]), b[1:], true
}
