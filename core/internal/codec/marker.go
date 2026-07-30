package codec

import "strings"

// marker prefixes the text value of every BoardProxy object. The trailing digit
// is the codec version so a future encoding can coexist with base64 during a
// migration; the ':' terminates it and never appears in base64 output.
const marker = "BPX1:"

// IsProtocolValue reports whether value carries the BoardProxy marker.
func IsProtocolValue(value string) bool {
	return strings.HasPrefix(value, marker)
}
