// main.go (Complete Refactored Version)
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Server encapsulates the dependencies for the HTTP server.
type Server struct {
	router         *http.ServeMux
	secretProvider SecretProvider // Dependency Injection via interface
}

// NewServer accepts a SecretProvider to allow for production vs mock usage.
func NewServer(sp SecretProvider) *Server {
	s := &Server{
		router:         http.NewServeMux(),
		secretProvider: sp,
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

	// Use the injected provider instead of os.LookupEnv directly
	if secret, err := s.secretProvider.GetSecret("SECRET"); err == nil && secret != "" {
		response = fmt.Sprintf("%s secret: %s", response, secret)
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
	// The Base implementation (Production)
	baseProvider := &EnvSecretProvider{}

	// The Decorator: Wrap the base provider with caching capabilities
	// Now, calls to GetSecret will only hit the OS environment if the cache is empty/expired.
	cachedProvider := NewCachedSecretProvider(baseProvider, 5*time.Minute)

	// Inject the decorated provider into the server
	srv := NewServer(cachedProvider)

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
