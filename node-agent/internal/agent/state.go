package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"bproxy-node-agent/internal/localstore"
)

const agentStateKey = "agent-state"

type appliedState struct {
	Revision uint64 `json:"applied_revision"`
	SHA256   string `json:"config_sha256"`
}

func loadState(store *localstore.Store) (appliedState, error) {
	raw, err := store.Checkpoint(agentStateKey)
	if err != nil || len(raw) == 0 {
		return appliedState{}, err
	}
	var state appliedState
	return state, json.Unmarshal(raw, &state)
}

func randomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}
