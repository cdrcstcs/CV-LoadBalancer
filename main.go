package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

type Server interface {
	Address() string
	IsAlive() bool
	Serve(rw http.ResponseWriter, req *http.Request)
}

type simpleServer struct {
	addr  string
	proxy *httputil.ReverseProxy
	alive bool
	mutex sync.RWMutex
}

func (s *simpleServer) Address() string {
	return s.addr
}

func (s *simpleServer) IsAlive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.alive
}

func (s *simpleServer) SetAlive(alive bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.alive = alive
}

func (s *simpleServer) Serve(rw http.ResponseWriter, req *http.Request) {
	s.proxy.ServeHTTP(rw, req)
}

func newSimpleServer(addr string) *simpleServer {
	serverURL, err := url.Parse(addr)
	handleErr(err)
	return &simpleServer{
		addr:  addr,
		proxy: httputil.NewSingleHostReverseProxy(serverURL),
		alive: true,
	}
}

type LoadBalancer struct {
	port            string
	roundRobinCount int
	servers         []Server
	mutex           sync.RWMutex
}

func NewLoadBalancer(port string) *LoadBalancer {
	return &LoadBalancer{
		port:            port,
		roundRobinCount: 0,
		servers:         []Server{},
	}
}

func (lb *LoadBalancer) AddServer(server Server) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	lb.servers = append(lb.servers, server)
}

func (lb *LoadBalancer) RemoveServer(addr string) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	for i, server := range lb.servers {
		if server.Address() == addr {
			lb.servers = append(lb.servers[:i], lb.servers[i+1:]...)
			return
		}
	}
}

func handleErr(err error) {
	if err != nil {
		log.Printf("error: %v\n", err)
	}
}

func (lb *LoadBalancer) getNextAvailableServer() Server {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()

	for len(lb.servers) == 0 {
		time.Sleep(100 * time.Millisecond)
	}

	for {
		server := lb.servers[lb.roundRobinCount%len(lb.servers)]
		lb.roundRobinCount++
		if server.IsAlive() {
			return server
		}
	}
}

func (lb *LoadBalancer) serveProxy(rw http.ResponseWriter, req *http.Request) {
	targetServer := lb.getNextAvailableServer()

	log.Printf("forwarding request to address %q\n", targetServer.Address())

	// Forward the request to the target server
	targetServer.Serve(rw, req)
}

func (lb *LoadBalancer) startHealthChecks(interval time.Duration) {
	go func() {
		for {
			lb.mutex.RLock()
			for _, server := range lb.servers {
				go lb.checkServerHealth(server)
			}
			lb.mutex.RUnlock()
			time.Sleep(interval)
		}
	}()
}

func (lb *LoadBalancer) checkServerHealth(server Server) {
	resp, err := http.Get(server.Address() + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		server.(*simpleServer).SetAlive(false)
		log.Printf("server %q is down\n", server.Address())
	} else {
		server.(*simpleServer).SetAlive(true)
	}
}

func (lb *LoadBalancer) serveConfigUpdate(rw http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodPost {
		addr := req.FormValue("address")
		action := req.FormValue("action")
		if addr == "" {
			http.Error(rw, "Address parameter missing", http.StatusBadRequest)
			return
		}
		if action == "add" {
			lb.AddServer(newSimpleServer(addr))
			fmt.Fprintf(rw, "Server added: %s\n", addr)
		} else if action == "remove" {
			lb.RemoveServer(addr)
			fmt.Fprintf(rw, "Server removed: %s\n", addr)
		} else {
			http.Error(rw, "Invalid action parameter", http.StatusBadRequest)
		}
	} else {
		http.Error(rw, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func main() {
	lb := NewLoadBalancer("8000")
	lb.AddServer(newSimpleServer("https://www.facebook.com"))
	lb.AddServer(newSimpleServer("https://www.bing.com"))
	lb.AddServer(newSimpleServer("https://www.duckduckgo.com"))

	// Start health checks every 10 seconds
	lb.startHealthChecks(10 * time.Second)

	http.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		lb.serveProxy(rw, req)
	})

	http.HandleFunc("/config", func(rw http.ResponseWriter, req *http.Request) {
		lb.serveConfigUpdate(rw, req)
	})

	log.Printf("serving requests at 'localhost:%s'\n", lb.port)
	handleErr(http.ListenAndServe(":"+lb.port, nil))
}
