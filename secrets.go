// secrets.go
package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// SecretProvider defines the abstraction for retrieving sensitive configuration.
type SecretProvider interface {
	GetSecret(key string) (string, error)
}

// EnvSecretProvider is the production implementation using OS environment variables.
type EnvSecretProvider struct{}

func (e *EnvSecretProvider) GetSecret(key string) (string, error) {
	val, exists := os.LookupEnv(key)
	if !exists {
		return "", fmt.Errorf("secret %s not found", key)
	}
	return val, nil
}

// CachedSecretProvider is a Decorator that wraps a SecretProvider
// and adds an in-memory cache with a TTL (Time To Live).
type CachedSecretProvider struct {
	inner SecretProvider
	ttl   time.Duration

	mu    sync.RWMutex // Protects the cache map
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

func NewCachedSecretProvider(inner SecretProvider, ttl time.Duration) *CachedSecretProvider {
	return &CachedSecretProvider{
		inner: inner,
		ttl:   ttl,
		cache: make(map[string]cacheEntry),
	}
}

func (c *CachedSecretProvider) GetSecret(key string) (string, error) {
	// 1. Attempt to read from cache with a Read Lock
	c.mu.RLock()
	entry, found := c.cache[key]
	if found && time.Now().Before(entry.expiresAt) {
		val := entry.value
		c.mu.RUnlock()
		return val, nil
	}
	c.mu.RUnlock()

	// 2. Cache miss or expired: Fetch from the inner provider
	val, err := c.inner.GetSecret(key)
	if err != nil {
		return "", err
	}

	// 3. Update cache with a Write Lock
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[key] = cacheEntry{
		value:     val,
		expiresAt: time.Now().Add(c.ttl),
	}

	return val, nil
}
