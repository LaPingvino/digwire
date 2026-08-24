package search

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	dnsMu              sync.RWMutex
	FallbackDNSServers = []string{
		"8.8.8.8:53",
		"1.1.1.1:53",
		"8.8.4.4:53",
		"1.0.0.1:53",
		"9.9.9.9:53",
	}
)

// SetFallbackDNSServers updates the active anti-censorship DNS servers list
func SetFallbackDNSServers(servers []string) {
	if len(servers) == 0 {
		return
	}
	dnsMu.Lock()
	defer dnsMu.Unlock()
	FallbackDNSServers = make([]string, len(servers))
	copy(FallbackDNSServers, servers)
}

// NewResilientHTTPClient creates an HTTP client with DNS fallback to bypass ISP/regional DNS blocking
func NewResilientHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   6 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Try default system resolver first with 1.5s timeout
				d := net.Dialer{Timeout: 1500 * time.Millisecond}
				c, err := d.DialContext(ctx, network, address)
				if err == nil {
					return c, nil
				}

				dnsMu.RLock()
				servers := make([]string, len(FallbackDNSServers))
				copy(servers, FallbackDNSServers)
				dnsMu.RUnlock()

				// Fallback to anti-censorship public resolvers (Google / Cloudflare / Quad9)
				for _, dnsServer := range servers {
					if conn, err := d.DialContext(ctx, "udp", dnsServer); err == nil {
						return conn, nil
					}
				}
				return nil, err
			},
		},
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:   false,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
