// TLS front for the disposable Telemt bench only. Its private CA is trusted
// inside that container; production nodes use their normal public certificate.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

func main() {
	host := flag.String("host", "fakenode-telemt.local", "disposable node hostname")
	prepare := flag.Bool("prepare", false, "write the private bench certificate before starting the agent")
	flag.Parse()
	const cert = "/usr/local/share/ca-certificates/tgwp-bench.crt"
	const key = "/etc/telemt/bench-tls.key"
	if *prepare {
		private, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal(err)
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			log.Fatal(err)
		}
		now := time.Now()
		template := &x509.Certificate{
			SerialNumber: serial, Subject: pkix.Name{CommonName: "TGProxy disposable bench"},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
			DNSNames: []string{*host}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
			BasicConstraintsValid: true, IsCA: true,
			KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}), 0o600); err != nil {
			log.Fatal(err)
		}
		return
	}
	upstream, _ := url.Parse("http://127.0.0.1:18080")
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	// Do not log request URLs: WEB capabilities are carried in the query.
	server := &http.Server{Addr: ":443", Handler: proxy, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	log.Fatal(server.ListenAndServeTLS(cert, key))
}
