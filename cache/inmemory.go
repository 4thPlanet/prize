package cache

import (
	"context"
	"sync"
	"time"
)

type cacheEntry struct {
	value   any
	mu      sync.Mutex
	expires *time.Time
}
type InMemoryCache struct {
	data   map[string]*cacheEntry
	mu     sync.RWMutex
	cancel context.CancelFunc
}

var _ Cache = new(InMemoryCache)

func NewInMemoryCache() *InMemoryCache {
	cache := &InMemoryCache{
		data: make(map[string]*cacheEntry),
		mu:   sync.RWMutex{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cache.cancel = cancel

	go cache.expireRecords(ctx)
	return cache
}
func (c *InMemoryCache) Del(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.data, key)
	}

	return nil
}

type KeyNotFoundError struct{}

func (KeyNotFoundError) Error() string { return "Key not found." }

func (c *InMemoryCache) Get(_ context.Context, key string) (any, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, isset := c.data[key]; isset {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		return entry.value, nil
	}
	return nil, KeyNotFoundError{}
}

func (c *InMemoryCache) Set(ctx context.Context, key string, val any, ttl *time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expires *time.Time
	if ttl != nil {
		expires = new(time.Now().Add(*ttl))
	}

	if entry, isset := c.data[key]; isset {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		entry.value = val
		entry.expires = expires
	} else {
		c.data[key] = &cacheEntry{
			value:   val,
			expires: expires,
		}
	}
	return nil

}
func (c *InMemoryCache) Atomic(ctx context.Context, key string, cb func(any) any, ttl *time.Duration) error {
	c.mu.RLock()
	var expires *time.Time
	if ttl != nil {
		expires = new(time.Now().Add(*ttl))
	}
	if entry, isset := c.data[key]; isset {
		defer c.mu.RUnlock()
		entry.mu.Lock()
		defer entry.mu.Unlock()
		value := cb(entry.value)
		entry.value = value
		entry.expires = expires
	} else {
		c.mu.RUnlock()
		c.mu.Lock()
		// Check one more time, to make sure some other request didn't add a cache entry
		entry, isset := c.data[key]
		var value any
		if !isset {
			entry = new(cacheEntry)
			entry.mu.Lock()
		} else {
			entry.mu.Lock()
			value = entry.value
		}

		defer entry.mu.Unlock()
		entry.expires = expires
		c.data[key] = entry
		c.mu.Unlock()
		entry.value = cb(value)
	}
	return nil
}
func (c *InMemoryCache) Close() error {
	defer c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*cacheEntry)
	return nil
}
func (c *InMemoryCache) expireRecords(ctx context.Context) {
	isDone := ctx.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {

		select {
		case <-isDone:
			return
		case <-t.C:

			now := time.Now()
			c.mu.RLock()
			potentialExpires := []string{}
			for key, entry := range c.data {
				if entry.expires != nil && entry.expires.Before(now) {
					potentialExpires = append(potentialExpires, key)
				}
			}
			c.mu.RUnlock()
			c.mu.Lock()
			for _, key := range potentialExpires {
				// One more check before deleting, in case a new entry snuck in between switching locks
				if entry, isset := c.data[key]; isset {
					entry.mu.Lock()
					if entry.expires != nil && entry.expires.Before(now) {
						delete(c.data, key)
					}
					entry.mu.Unlock()
				}
			}
			c.mu.Unlock()
		}
	}

}
