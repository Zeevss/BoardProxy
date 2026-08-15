package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const fragmentPrefix = "bp1="

var ErrInvalidCapsule = errors.New("invalid BoardProxy recovery capsule")

type Capsule struct {
	Version              int    `json:"version"`
	YandexURL            string `json:"yandexUrl"`
	RecoveryKeyID        string `json:"recoveryKeyId"`
	ClientPrivateKey     string `json:"clientPrivateKey"`
	RecoveryServerPublic string `json:"recoveryServerPublic"`
}

func BuildURL(baseURL, token string, capsule Capsule) (string, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/s/" + url.PathEscape(token))
	if err != nil {
		return "", fmt.Errorf("parse public URL: %w", err)
	}
	encoded, err := capsule.Encode()
	if err != nil {
		return "", err
	}
	base.Fragment = fragmentPrefix + encoded
	return base.String(), nil
}

func ParseURL(raw string) (*url.URL, string, Capsule, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, "", Capsule{}, fmt.Errorf("%w: malformed subscription URL", ErrInvalidCapsule)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-2] != "s" || parts[len(parts)-1] == "" {
		return nil, "", Capsule{}, fmt.Errorf("%w: subscription token is missing", ErrInvalidCapsule)
	}
	token, err := url.PathUnescape(parts[len(parts)-1])
	if err != nil {
		return nil, "", Capsule{}, fmt.Errorf("%w: invalid subscription token", ErrInvalidCapsule)
	}
	if !strings.HasPrefix(parsed.Fragment, fragmentPrefix) {
		return nil, "", Capsule{}, fmt.Errorf("%w: recovery fragment is missing", ErrInvalidCapsule)
	}
	capsule, err := DecodeCapsule(strings.TrimPrefix(parsed.Fragment, fragmentPrefix))
	if err != nil {
		return nil, "", Capsule{}, err
	}
	requestURL := *parsed
	requestURL.Fragment = ""
	return &requestURL, token, capsule, nil
}

func (c Capsule) Encode() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode recovery capsule: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCapsule(encoded string) (Capsule, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Capsule{}, fmt.Errorf("%w: invalid base64", ErrInvalidCapsule)
	}
	var capsule Capsule
	if err := json.Unmarshal(raw, &capsule); err != nil {
		return Capsule{}, fmt.Errorf("%w: invalid JSON", ErrInvalidCapsule)
	}
	if err := capsule.Validate(); err != nil {
		return Capsule{}, err
	}
	return capsule, nil
}

func (c Capsule) Validate() error {
	if c.Version != 1 || c.RecoveryKeyID == "" {
		return fmt.Errorf("%w: unsupported version or empty key id", ErrInvalidCapsule)
	}
	if u, err := url.Parse(c.YandexURL); err != nil || u.Scheme != "https" || !allowedYandexHost(u.Hostname()) {
		return fmt.Errorf("%w: invalid Yandex URL", ErrInvalidCapsule)
	}
	if !validKey(c.ClientPrivateKey) || !validKey(c.RecoveryServerPublic) {
		return fmt.Errorf("%w: recovery keys must contain 32 raw-url-base64 bytes", ErrInvalidCapsule)
	}
	return nil
}

func allowedYandexHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "disk.yandex.ru" || host == "docs.yandex.ru" ||
		host == "disk.yandex.com" || host == "docs.yandex.com"
}

func DecodeKey(encoded string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("%w: invalid recovery key", ErrInvalidCapsule)
	}
	return decoded, nil
}

func EncodeKey(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

func validKey(encoded string) bool {
	raw, err := DecodeKey(encoded)
	if err != nil {
		return false
	}
	for _, value := range raw {
		if value != 0 {
			return true
		}
	}
	return false
}
