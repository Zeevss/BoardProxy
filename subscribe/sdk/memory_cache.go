package sdk

import (
	"sync"

	"github.com/Zeevss/BoardProxy/subscribe/protocol"
)

type MemoryCache struct {
	mu     sync.RWMutex
	values map[string]protocol.Subscription
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{values: make(map[string]protocol.Subscription)}
}

func (c *MemoryCache) Load(subscriptionURL string) (protocol.Subscription, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.values[subscriptionURL]
	return value, ok
}

func (c *MemoryCache) Store(subscriptionURL string, value protocol.Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[subscriptionURL] = value
}
