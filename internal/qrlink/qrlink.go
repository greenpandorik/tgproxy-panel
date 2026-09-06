// Package qrlink builds WEB proxy links and QR codes.
package qrlink

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"
)

func TMe(host, secret string) string {
	return "https://t.me/webproxy?server=" + url.QueryEscape(host) + "&secret=" + url.QueryEscape(secret)
}

func Tg(host, secret string) string {
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

// FakeTLSSecret builds the secret a Telegram client needs for a Fake-TLS
// (MTProto "ee") connection: the "ee" marker, the 32-hex-character proxy
// secret, and the SNI domain hex-encoded. Clients slice it back apart by those
// fixed offsets, so the three parts are concatenated with no separator.
func FakeTLSSecret(hexSecret, domain string) string {
	return "ee" + hexSecret + hex.EncodeToString([]byte(domain))
}

// TMeProxy is the https://t.me link for a classic MTProto listener (Fake-TLS on
// telemt nodes), as opposed to TMe which links a WEB-transport proxy.
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
