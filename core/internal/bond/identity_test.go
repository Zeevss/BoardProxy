package bond

import "testing"

func TestBundleIDRoundTrip(t *testing.T) {
	id, err := NewBundleID()
	if err != nil {
		t.Fatal(err)
	}
	if id.IsZero() {
		t.Fatal("generated zero BundleID")
	}
	parsed, err := ParseBundleID(id.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != id {
		t.Fatalf("parsed id = %v, want %v", parsed, id)
	}
}

func TestParseBundleIDRejectsMalformedValue(t *testing.T) {
	for _, raw := range []string{"", "xyz", "0011"} {
		if _, err := ParseBundleID(raw); err == nil {
			t.Fatalf("ParseBundleID(%q) succeeded", raw)
		}
	}
}

func TestJoinTokenEquality(t *testing.T) {
	token, err := NewJoinToken()
	if err != nil {
		t.Fatal(err)
	}
	if !token.Equal(token) {
		t.Fatal("token does not equal itself")
	}
	other := token
	other[0] ^= 1
	if token.Equal(other) {
		t.Fatal("different tokens compare equal")
	}
}
