// main.go (Complete Refactored Version)
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// Server encapsulates the dependencies for the HTTP server.
type Server struct {
	router           *http.ServeMux
	secretProviders []SecretProvider // Dependency Injection via interface
}

// NewServer accepts a list of SecretProviders
// to allow for Chain of Responsibility in production and mock testing
func NewServer(providers ...SecretProvider) *Server {
	s := &Server{
		router:          http.NewServeMux(),
		secretProviders: providers,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.HandleFunc("/", s.handleHello)
}

func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	response := fmt.Sprintf("hello world %s", timestamp)


    // Iterate through providers in the order they were passed to NewServer
    var foundSecret string
    for _, provider := range s.secretProviders {
        secret, err := provider.GetSecret("SECRET")
        if err == nil && secret != "" {
            foundSecret = secret
            break // Stop at the first successful retrieval
        }
    }

    if foundSecret != "" {
        response = fmt.Sprintf("%s secret: %s", response, foundSecret)
    }

    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(response)); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func main() {
	// Define our prioritized chain of providers
    var providers []SecretProvider
    const cacheTimeout = 5*time.Minute

    // 1. High Priority: Check File-based (tmpfs) first
    if filePath := os.Getenv("SECRET_FILE_PATH"); filePath != "" {
        log.Printf("Registering FileSecretProvider: %s", filePath)
        // The Decorator: Wrap the provider with caching capabilities
        providers = append(providers, NewCachedSecretProvider(&FileSecretProvider{filePath: filePath}, cacheTimeout))
    }

    // 2. Low Priority: Fallback to Environment Variables
    log.Println("Registering EnvSecretProvider")
    providers = append(providers, NewCachedSecretProvider(&EnvSecretProvider{}, cacheTimeout))

	// Inject the providers into the server
	srv := NewServer(providers...)

	addr := ":8080"
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Starting server on %s", addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen error: %v", err)
	}
}
