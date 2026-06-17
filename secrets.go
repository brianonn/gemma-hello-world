// secrets.go
package main

import (
	"fmt"
	"os"
	"sync"
	"time"
	"strings"
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

// FileSecretProvider is the production secret provider that
// gets the secret from a file, usually a mounted /tmpfs or /dev/shm file
type FileSecretProvider struct {
    filePath string
}

func (f *FileSecretProvider) GetSecret(key string) (string, error) {
    data, err := os.ReadFile(f.filePath)
    if err != nil {
        return "", fmt.Errorf("failed to read secret file: %w", err)
    }

    // Parses a simple key=value format from the file
    lines := strings.Split(string(data), "\n")
    for _, line := range lines {
        parts := strings.SplitN(line, "=", 2)
        if len(parts) == 2 && parts[0] == key {
            return parts[1], nil
        }
    }

    return "", fmt.Errorf("key %s not found in %s", key, f.filePath)
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
