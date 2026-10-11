// Package qrlink builds WEB proxy links and QR codes.
package qrlink

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"
)

func TMe(host, secret string, basePath ...string) string {
	host, secret = webLinkParts(host, secret, basePath)
	return "https://t.me/webproxy?server=" + url.QueryEscape(host) + "&secret=" + url.QueryEscape(secret)
}

func Tg(host, secret string, basePath ...string) string {
	host, secret = webLinkParts(host, secret, basePath)
	return "tg://webproxy?server=" + url.QueryEscape(host) + "&secret=" + url.QueryEscape(secret)
}

func PNG(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, size)
}

func DataURI(content string, size int) (string, error) {
	png, err := PNG(content, size)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func FakeTLSSecret(hexSecret, domain string) string {
	return "ee" + hexSecret + hex.EncodeToString([]byte(domain))
}

func TMeProxy(host string, port int, secret string) string {
	return "https://t.me/proxy?" + proxyQuery(host, port, secret)
}

// TgProxy is the tg:// twin of TMeProxy, for clients that handle the scheme.
func TgProxy(host string, port int, secret string) string {
	return "tg://proxy?" + proxyQuery(host, port, secret)
}

func proxyQuery(host string, port int, secret string) string {
	return "server=" + url.QueryEscape(host) + "&port=" + strconv.Itoa(port) + "&secret=" + url.QueryEscape(secret)
}

func webLinkParts(host, secret string, basePath []string) (string, string) {
	if len(basePath) == 0 || basePath[0] == "" {
		return host, secret
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return host, secret
	}
	return host + "/" + basePath[0], base64.RawURLEncoding.EncodeToString(append([]byte{0x70}, raw...))
}
