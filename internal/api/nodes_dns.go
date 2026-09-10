package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleNodeDNS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hostname string `json:"hostname"`
		PublicIP string `json:"public_ip"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err.Error())
		return
	}
	host := strings.TrimSpace(body.Hostname)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/:@ ") {
		validation(w, map[string]string{"hostname": "a DNS hostname is required"})
		return
	}
	var expected net.IP
	if body.PublicIP != "" {
		expected = net.ParseIP(body.PublicIP)
		if expected == nil {
			validation(w, map[string]string{"public_ip": "invalid IP address"})
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	addresses := []string{}
	matched := false
	for _, ip := range ips {
		addresses = append(addresses, ip.IP.String())
		if expected == nil || ip.IP.Equal(expected) {
			matched = true
		}
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	writeJSON(w, 200, map[string]any{"hostname": host, "addresses": addresses, "matches": matched, "expected_ip": body.PublicIP, "error": detail})
}
