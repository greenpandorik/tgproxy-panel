package subscription

import (
	"strconv"
	"strings"
	"sync"

	"tgwebproxy/internal/qrlink"
)

const maxQRCached = 4096

var qrCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// qrFor draws a link's QR code once and keeps it, so a busy page is not redrawn on every visit.
func qrFor(link string, size int) (string, error) {
	key := strconv.Itoa(size) + " " + link
	qrCache.Lock()
	if v, ok := qrCache.m[key]; ok {
		qrCache.Unlock()
		return v, nil
	}
	qrCache.Unlock()
	v, err := qrlink.DataURI(link, size)
	if err != nil {
		return "", err
	}
	qrCache.Lock()
	if len(qrCache.m) >= maxQRCached {
		qrCache.m = map[string]string{}
	}
	qrCache.m[key] = v
	qrCache.Unlock()
	return v, nil
}

// proxyLink reports whether a link is one the page may show as a button: a t.me proxy link or a
// tg:// link, nothing else.
func proxyLink(tme, tg string) bool {
	return strings.HasPrefix(tme, "https://t.me/") && strings.HasPrefix(tg, "tg://")
}
