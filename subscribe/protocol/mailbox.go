package protocol

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
)

const (
	mailboxColumns = 702 // A..ZZ
	mailboxRows    = 1000
	mailboxStart   = 2
)

func MailboxCell(requestID string, attempt int) string {
	digest := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(attempt)))
	index := int(binary.BigEndian.Uint64(digest[:8]) % uint64(mailboxColumns*mailboxRows))
	column := index % mailboxColumns
	row := mailboxStart + index/mailboxColumns
	return columnName(column) + strconv.Itoa(row)
}

func columnName(zeroBased int) string {
	value := zeroBased + 1
	result := ""
	for value > 0 {
		value--
		result = string(rune('A'+value%26)) + result
		value /= 26
	}
	return result
}
