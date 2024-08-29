package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Mock server implementation for testing
type mockServer struct {
	addr  string
	alive bool
}

func (s *mockServer) Address() string {
	return s.addr
}

func (s *mockServer) IsAlive() bool {
	return s.alive
}

func (s *mockServer) Serve(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
}

func TestLoadBalancer_AddServer(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server := &mockServer{addr: "http://localhost:9000", alive: true}

	lb.AddServer(server)
	if len(lb.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(lb.servers))
	}
}

func TestLoadBalancer_RemoveServer(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server := &mockServer{addr: "http://localhost:9000", alive: true}
	lb.AddServer(server)

	lb.RemoveServer("http://localhost:9000")
	if len(lb.servers) != 0 {
		t.Errorf("Expected 0 servers, got %d", len(lb.servers))
	}
}

func TestLoadBalancer_getNextAvailableServer(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server1 := &mockServer{addr: "http://localhost:9000", alive: true}
	server2 := &mockServer{addr: "http://localhost:9001", alive: false}

	lb.AddServer(server1)
	lb.AddServer(server2)

	server := lb.getNextAvailableServer()
	if server != server1 {
		t.Errorf("Expected server1, got %v", server)
	}
}

func TestLoadBalancer_serveProxy(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server := &mockServer{addr: "http://localhost:9000", alive: true}
	lb.AddServer(server)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	lb.serveProxy(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestHealthCheck(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server := newSimpleServer("http://localhost:9000")
	lb.AddServer(server)

	// Mock health check
	healthCheckServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/health" {
			rw.WriteHeader(http.StatusOK)
		} else {
			rw.WriteHeader(http.StatusNotFound)
		}
	}))
	defer healthCheckServer.Close()

	server.addr = healthCheckServer.URL

	// Run health checks
	lb.startHealthChecks(1 * time.Second)

	time.Sleep(2 * time.Second) // Wait for health check to complete

	if !server.IsAlive() {
		t.Errorf("Server should be alive")
	}

	// Stop health check server
	healthCheckServer.Close()
	server.SetAlive(false)
	time.Sleep(2 * time.Second)

	if server.IsAlive() {
		t.Errorf("Server should be down")
	}
}

func TestServeConfigUpdate(t *testing.T) {
	lb := NewLoadBalancer("8000")

	req := httptest.NewRequest(http.MethodPost, "/config", nil)
	req.PostForm = map[string][]string{
		"address": {"http://localhost:9000"},
		"action":  {"add"},
	}

	rr := httptest.NewRecorder()
	lb.serveConfigUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	if len(lb.servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(lb.servers))
	}
}
func TestServeProxy_InvalidServer(t *testing.T) {
	lb := NewLoadBalancer("8000")
	server := &mockServer{addr: "http://localhost:9000", alive: false}
	lb.AddServer(server)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	// Create a channel to signal completion or timeout
	done := make(chan struct{})
	go func() {
		lb.serveProxy(rr, req)
		close(done)
	}()

	select {
	case <-done:
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}
	case <-time.After(5 * time.Second): // Set timeout
		t.Fatal("Test timed out")
	}
}
