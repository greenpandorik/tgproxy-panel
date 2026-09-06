// Command stubproxy stands in for the real MTProxy binary inside the fakenode
// image. It accepts and holds TCP connections on 127.0.0.1:2398 (the loopback
// backend every profile points at, so tproxy-server's /readyz TCP dial and
// its data-plane connections both succeed) and serves a tiny tab-separated
// /stats page on 127.0.0.1:8888, mirroring the handful of counters the real
// mtproto-proxy exposes there.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"
)

const (
	tcpAddr  = "127.0.0.1:2398"
	httpAddr = "127.0.0.1:8888"
)

func main() {
	var accepted int64

	ln, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("stubproxy: listen %s: %v", tcpAddr, err)
	}
	go serveTCP(ln, &accepted)

	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "accepted_connections\t%d\n", atomic.LoadInt64(&accepted))
		_, _ = fmt.Fprintf(w, "version\tstub-1.0\n")
		_, _ = fmt.Fprintf(w, "uptime\t0\n")
	})
	log.Printf("stubproxy: tcp=%s http=%s", tcpAddr, httpAddr)
	if err := http.ListenAndServe(httpAddr, mux); err != nil { //nolint:gosec // loopback-only dev stub, no timeouts needed
		log.Fatalf("stubproxy: http %s: %v", httpAddr, err)
	}
}

// serveTCP accepts connections and holds them open (reading until the peer
// closes) so tproxy-server sees a live backend without needing real MTProxy
// framing.
func serveTCP(ln net.Listener, accepted *int64) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("stubproxy: accept: %v", err)
			return
		}
		atomic.AddInt64(accepted, 1)
		go hold(conn)
	}
}

func hold(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	buf := make([]byte, 4096)
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}
