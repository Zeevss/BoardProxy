package filesystem

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"time"

	"bproxy-control-plane/internal/domain"
)

type tokenRecord struct {
	NodeID    string    `json:"node_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Store) CreateEnrollmentToken(_ context.Context, nodeID string, ttl time.Duration) (string, error) {
	if !domain.ValidID(nodeID) {
		return "", domain.ErrInvalidID
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	record := tokenRecord{NodeID: nodeID, ExpiresAt: time.Now().UTC().Add(ttl)}
	return token, writeJSONExclusive(s.path("tokens", tokenFilename(token)), record)
}

func (s *Store) ConsumeEnrollmentToken(_ context.Context, nodeID, token string) error {
	name := tokenFilename(token)
	path := s.path("tokens", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.ErrTokenInvalid
	}
	var record tokenRecord
	if json.Unmarshal(raw, &record) != nil || record.NodeID != nodeID {
		return domain.ErrTokenInvalid
	}
	if time.Now().After(record.ExpiresAt) {
		return domain.ErrTokenExpired
	}
	if err := os.Rename(path, s.path("tokens-used", name)); err != nil {
		return domain.ErrTokenInvalid
	}
	return nil
}

func tokenFilename(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:]) + ".json"
}
