package bypass

import "testing"

func TestMatch(t *testing.T) {
	m, err := New([]string{`\.example\.com$`, `^10\.`})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		in   string
		want bool
	}{
		{"api.example.com:443", true},
		{"example.com:80", true}, // "example.com" оканчивается на ".example.com"? нет — проверим отдельно
		{"10.0.0.1:8080", true},
		{"google.com:443", false},
		{"sub.example.com", true},
		{"11.0.0.1", false},
	}
	// "example.com" не оканчивается на ".example.com" — поправим ожидание.
	cases[1].want = false
	for _, c := range cases {
		if got := m.Match(c.in); got != c.want {
			t.Errorf("Match(%q) = %v, хочу %v", c.in, got, c.want)
		}
	}
}

func TestUpdateAtomicOnError(t *testing.T) {
	m, _ := New([]string{`^a`})
	if !m.Match("abc:1") {
		t.Fatal("до Update: 'abc' должен совпадать с ^a")
	}
	// Битый паттерн — Update обязан вернуть ошибку и не тронуть список.
	if err := m.Update([]string{`^b`, `(`}); err == nil {
		t.Fatal("Update с битым regexp должен вернуть ошибку")
	}
	if !m.Match("abc:1") {
		t.Fatal("после неудачного Update старый список должен сохраниться")
	}
	// Корректный Update — список заменяется.
	if err := m.Update([]string{`^b`}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if m.Match("abc:1") || !m.Match("bcd:1") {
		t.Fatal("после Update должен действовать новый список")
	}
}

func TestEmptyNeverMatches(t *testing.T) {
	m, _ := New(nil)
	if m.Match("anything:443") {
		t.Fatal("пустой список не должен ничего пропускать")
	}
}
