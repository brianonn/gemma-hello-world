// main_test.go
package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"os"
	"testing"
	"time"
)

// MockSecretProvider allows us to control secret values without touching the OS env.
type MockSecretProvider struct {
	secrets map[string]string
}

func (m *MockSecretProvider) GetSecret(key string) (string, error) {
	val, exists := m.secrets[key]
	if !exists {
		return "", fmt.Errorf("not found")
	}
	return val, nil
}

func TestHandleHello(t *testing.T) {

	// Regex to validate RFC3339 (ISO8601) format: YYYY-MM-DDTHH:MM:SSZ or with offset
	// Matches: 2024-05-20T15:04:05Z or 2024-05-20T15:04:05+07:00
	timestampRegex := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})`)

	tests := []struct {
		name            string
		mockSecrets     map[string]string // secrets store
		wantPrefix      string            // part of string that should be at the start
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "Case 1: No SECRET environment variable",
			mockSecrets:     map[string]string{"OTHER": "value"},
			wantPrefix:      "hello world ",
			wantNotContains: []string{"secret: "},
		},
		{
			name:         "Case 2: SECRET environment variable is present",
			mockSecrets:  map[string]string{"SECRET": "my-super-secure-token"},
			wantPrefix:   "hello world ",
			wantContains: []string{"secret: my-super-secure-token"},
		},
		{
			name:         "Case 3: Extremely large SECRET environment variable",
			mockSecrets:  map[string]string{"SECRET": strings.Repeat("A", 1024*1024)}, // a 1MB string of 'A's
			wantPrefix:   "hello world ",
			wantContains: []string{"secret: " + strings.Repeat("A", 1024*1024)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Inject the mock provider
			mockProvider := &MockSecretProvider{secrets: tt.mockSecrets}
			srv := NewServer(mockProvider)

			// Create Request and Recorder
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			// Execute Handler
			srv.handleHello(w, req)

			// Analyze Response
			resp := w.Result()
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}
			body := string(bodyBytes)

			// 1. Verify Status Code
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			// 2. Verify Timestamp format via regex exists in the response
			if !timestampRegex.MatchString(body) {
				t.Errorf("response body does not contain a valid ISO8601 timestamp: %s", body)
			}

			// 3. Verify Prefix
			if !strings.HasPrefix(body, tt.wantPrefix) {
				t.Errorf("expected prefix %q, got %q", tt.wantPrefix, body)
			}

			// 4. Verify Contains (for the secret part)
			for _, want := range tt.wantContains {
				if !strings.Contains(body, want) {
					t.Errorf("expected body to contain %q, but it did not. Body: %s", want, body)
				}
			}

			// 5. Verify that 'secret:' does not appear when no secret is provided
			for _, notwant := range tt.wantNotContains {
				if strings.Contains(body, notwant) {
					t.Errorf("expected body NOT to contain %q, but it did. Body: %s", notwant, body)
				}
			}
		})
	}
}

// ErrorSecretProvider simulates a systemic failure (e.g., Vault is down)
type ErrorSecretProvider struct{}

func (e *ErrorSecretProvider) GetSecret(key string) (string, error) {
	return "", errors.New("internal vault connection error")
}

func TestHandleHello_ProviderError(t *testing.T) {
	// Setup server with the failing provider
	errProvider := &ErrorSecretProvider{}
	srv := NewServer(errProvider)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	// Execute handler
	srv.handleHello(w, req)

	resp := w.Result()
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	// 1. Verify that the server still returns 200 OK despite the provider error
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// 2. Verify the 'hello world' part is still present
	if !strings.HasPrefix(body, "hello world") {
		t.Errorf("expected prefix 'hello world', got %q", body)
	}

	// 3. CRITICAL: Verify that the error did NOT leak into the response via the secret string
	// The handler should have silently ignored the error and skipped the secret block
	if strings.Contains(body, "secret:") {
		t.Errorf("expected no 'secret:' string in body when provider fails, but found: %q", body)
	}

	if strings.Contains(body, "internal vault connection error") {
		t.Error("security leak: raw error message from SecretProvider was leaked to the HTTP response")
	}
}

// SpySecretProvider counts how many times GetSecret is actually called
type SpySecretProvider struct {
	callCount int
	value     string
}

func (s *SpySecretProvider) GetSecret(key string) (string, error) {
	s.callCount++
	return s.value, nil
}

func TestCachedSecretProvider_Decorator(t *testing.T) {
	spy := &SpySecretProvider{value: "cached-value"}
	// Wrap spy with a cache that has a short TTL
	cacheDecorator := NewCachedSecretProvider(spy, 1*time.Second)

	// First call: Should hit the Spy (Cache Miss)
	val1, err := cacheDecorator.GetSecret("SECRET")
	if err != nil || val1 != "cached-value" {
		t.Errorf("First call failed: %v", err)
	}
	if spy.callCount != 1 {
		t.Errorf("Expected 1 call to spy, got %d", spy.callCount)
	}

	// Second call: Should hit the Cache (Cache Hit)
	val2, err := cacheDecorator.GetSecret("SECRET")
	if err != nil || val2 != "cached-value" {
		t.Errorf("Second call failed: %v", err)
	}
	if spy.callCount != 1 {
		t.Errorf("Expected call count to remain 1, but it increased to %d", spy.callCount)
	}

	// Wait for TTL to expire
	time.Sleep(1500 * time.Millisecond)

	// Third call: Should hit the Spy again (Cache Expired/Miss)
	val3, err := cacheDecorator.GetSecret("SECRET")
	if err != nil || val3 != "cached-value" {
		t.Errorf("Third call failed after expiry: %v", err)
	}
	if spy.callCount != 2 {
		t.Errorf("Expected call count to increment to 2, got %d", spy.callCount)
	}
}

func TestFileSecretProvider(t *testing.T) {
	// Create a temporary file
	content := "SECRET=my-file-secret\nOTHER=other-string"
	tmpfile, err := os.CreateTemp("", "secret_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	provider := &FileSecretProvider{filePath: tmpfile.Name()}

	// Test finding the secret
	val, err := provider.GetSecret("SECRET")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if val != "my-file-secret" {
		t.Errorf("expected my-file-secret, got %s", val)
	}

	// Test finding a non-existent key
	val, err = provider.GetSecret("NON_EXISTENT")
	if err == nil {
		t.Error("expected error for non-existent key, but got none")
	}
}

func TestCachedFileSecretProvider(t *testing.T) {
	// Create a temporary file to act as the secret source
	content := "SECRET=cached-file-secret"
	tmpfile, err := os.CreateTemp("", "cached_secret_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	// Initialize the base provider and wrap it with a cache
	baseProvider := &FileSecretProvider{filePath: tmpfile.Name()}
	cacheTTL := 1 * time.Second
	cachedProvider := NewCachedSecretProvider(baseProvider, cacheTTL)

	// First call (Cache Miss): Should read from the file
	val1, err := cachedProvider.GetSecret("SECRET")
	if err != nil {
		t.Errorf("First call failed: %v", err)
	}
	if val1 != "cached-file-secret" {
		t.Errorf("expected cached-file-secret, got %s", val1)
	}

	// Second call (Cache Hit): Should read from the in-memory cache
	val2, err := cachedProvider.GetSecret("SECRET")
	if err != nil {
		t.Errorf("Second call failed: %v", err)
	}
	if val2 != "cached-file-secret" {
		t.Errorf("expected cached-file-secret, got %s", val2)
	}

	// Wait for TTL to expire so we can test a cache miss/refresh
	time.Sleep(1500 * time.Millisecond)

	// Third call (Cache Expired): Should read from the file again
	val3, err := cachedProvider.GetSecret("SECRET")
	if err != nil {
		t.Errorf("Third call after expiry failed: %v", err)
	}
	if val3 != "cached-file-secret" {
		t.Errorf("expected cached-file-secret after expiry, got %s", val3)
	}
}

// SpyFileSecretProvider wraps a FileSecretProvider to track call counts.
type SpyFileSecretProvider struct {
	base      *FileSecretProvider
	callCount int
}

func (s *SpyFileSecretProvider) GetSecret(key string) (string, error) {
	s.callCount++
	return s.base.GetSecret(key)
}

func TestCachedFileSecretProvider_Decorator(t *testing.T) {
	// 1. Setup: Create a temporary file with secret content
	content := "SECRET=cached-file-secret"
	tmpfile, err := os.CreateTemp("", "cached_secret_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	// 2. Initialize the chain: FileSecretProvider -> Spy -> CachedSecretProvider
	baseProvider := &FileSecretProvider{filePath: tmpfile.Name()}
	spy := &SpyFileSecretProvider{base: baseProvider}
	cacheTTL := 1 * time.Second
	cachedDecorator := NewCachedSecretProvider(spy, cacheTTL)

	// 3. First call: Should be a Cache Miss (calls the spy and the underlying file provider)
	val1, err := cachedDecorator.GetSecret("SECRET")
	if err != nil || val1 != "cached-file-secret" {
		t.Errorf("First call failed: got %q, err: %v", val1, err)
	}
	if spy.callCount != 1 {
		t.Errorf("Expected 1 call to underlying provider on cache miss, got %d", spy.callCount)
	}

	// 4. Second call: Should be a Cache Hit (does NOT call the spy or file provider)
	val2, err := cachedDecorator.GetSecret("SECRET")
	if err != nil || val2 != "cached-file-secret" {
		t.Errorf("Second call failed: got %q, err: %v", val2, err)
	}
	if spy.callCount != 1 {
		t.Errorf("Expected call count to remain 1 on cache hit, but it increased to %d", spy.callCount)
	}

	// 5. Wait for TTL to expire
	time.Sleep(1500 * time.Millisecond)

	// 6. Third call: Should be a Cache Miss/Refresh (calls the spy again)
	val3, err := cachedDecorator.GetSecret("SECRET") // Note: I'll ensure it calls GetSecret correctly
	if err != nil || val3 != "cached-file-secret" {
		t.Errorf("Third call after expiry failed: got %q, err: %v", val3, err)
	}
	if spy.callCount != 2 {
		t.Errorf("Expected call count to increment to 2 after cache expiry, got %d", spy.callCount)
	}
}
