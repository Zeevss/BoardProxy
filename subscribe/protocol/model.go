package protocol

import "time"

const MediaType = "application/vnd.boardproxy.subscription+json"

type Subscription struct {
	Version      int       `json:"version"`
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	State        string    `json:"state"`
	Revision     string    `json:"revision"`
	IssuedAt     time.Time `json:"issuedAt"`
	UsedBytes    uint64    `json:"usedBytes"`
	TrafficLimit uint64    `json:"trafficLimit,omitempty"`
	Keys         []Key     `json:"keys"`
}

type Key struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	NodeID    string `json:"nodeId"`
	UserID    string `json:"userId"`
	State     string `json:"state"`
	UsedBytes uint64 `json:"usedBytes"`
	Keylink   string `json:"keylink,omitempty"`
}

func (s Subscription) EnabledKeys() []Key {
	result := make([]Key, 0, len(s.Keys))
	for _, key := range s.Keys {
		if key.State == "enabled" && key.Keylink != "" {
			result = append(result, key)
		}
	}
	return result
}

type ClientHello struct {
	Version         int       `json:"version"`
	RequestID       string    `json:"requestId"`
	CreatedAt       time.Time `json:"createdAt"`
	RequestedFormat string    `json:"requestedFormat"`
	SDKVersion      string    `json:"sdkVersion,omitempty"`
}

type ServerHello struct {
	Version      int            `json:"version"`
	RequestID    string         `json:"requestId"`
	Subscription Subscription   `json:"subscription"`
	Error        *RecoveryError `json:"error,omitempty"`
}

type RecoveryError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
