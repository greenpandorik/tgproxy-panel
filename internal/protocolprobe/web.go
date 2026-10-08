package protocolprobe

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

var (
	bootstrapPattern = regexp.MustCompile(`\bbootstrap="([A-Za-z0-9_-]{43})"`)
	tokenPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

func webCapability(host, base string, secret []byte) string {
	label := "tdesktop-web-proxy-bridge-v1\n" + host
	if base != "" {
		label = "tdesktop-web-proxy-bridge-v2\n" + host + "\n" + base
	}
	return base64.RawURLEncoding.EncodeToString(hmacSHA(secret, []byte(label)))
}

func sharedFrame(kind byte, id uint32, p []byte) []byte {
	b := make([]byte, 8+len(p))
	b[0] = kind
	b[1] = byte(id >> 16)
	b[2] = byte(id >> 8)
	b[3] = byte(id)
	binary.BigEndian.PutUint32(b[4:8], uint32(len(p)))
	copy(b[8:], p)
	return b
}

func webBody(response *http.Response, limit int) ([]byte, error) {
	defer func() { _ = response.Body.Close() }()
	body, e := io.ReadAll(io.LimitReader(response.Body, int64(limit+1)))
	if e != nil || len(body) > limit {
		return nil, errProtocol
	}
	return body, nil
}

func webRequest(ctx context.Context, client *http.Client, method, endpoint, token string, body []byte, headers map[string]string) (*http.Response, error) {
	request, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, errProtocol
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return client.Do(request)
}

func probeWEB(ctx context.Context, host string, t Transport, secret, request []byte, x *mtExchange, client *http.Client) error {
	port := t.Port
	if port == 0 {
		port = 443
	}
	authority := host
	if port != 443 {
		authority = net.JoinHostPort(host, strconv.Itoa(port))
	}
	origin := "https://" + authority
	base := origin + "/"
	if t.BasePath != "" {
		base += t.BasePath + "/"
	}
	endpoint := strings.TrimSuffix(base, "/") + "/api/v1/"
	if client == nil {
		transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true}
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, e := webRequest(ctx, client, "GET", base+"?bridge="+webCapability(host, t.BasePath, secret), "", nil, nil)
	if e != nil {
		return e
	}
	body, e := webBody(response, 2*1024*1024)
	if e != nil || response.StatusCode != 200 {
		return errProtocol
	}
	match := bootstrapPattern.FindSubmatch(body)
	if len(match) != 2 {
		return errProtocol
	}
	response, e = webRequest(ctx, client, "POST", endpoint+"session", string(match[1]), sharedFrame(0x10, 0, []byte{1}), nil)
	if e != nil {
		return e
	}
	body, e = webBody(response, 64)
	if e != nil || response.StatusCode != 200 || !bytes.Equal(body, []byte{0x11, 0, 0, 0, 0, 0, 0, 0}) || response.Header.Get("X-Down-Cursor") != "0" {
		return errProtocol
	}
	token := response.Header.Get("X-Session-Token")
	carrier := response.Header.Get("X-Carrier-Mode")
	if !tokenPattern.MatchString(token) {
		return errProtocol
	}
	// A cancelled or malformed check must also release its short-lived WEB session.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		r, e := webRequest(cleanup, client, "DELETE", endpoint+"session", token, nil, nil)
		if e == nil {
			_ = r.Body.Close()
		}
	}()
	batch := append(sharedFrame(1, 1, nil), sharedFrame(2, 1, request)...)
	switch carrier {
	case "https", "https-lanes":
		return checkHTTPStream(ctx, client, endpoint, token, carrier, batch, x)
	case "websocket", "websocket-lanes":
		return checkWebSocketStream(ctx, client, origin, endpoint, token, carrier, batch, x)
	default:
		return errProtocol
	}
}

func acceptSharedFrames(body []byte, data *[]byte) ([]byte, error) {
	var pongs []byte
	count := 0
	for len(body) > 0 {
		count++
		if len(body) < 8 || count > 4096 {
			return nil, errProtocol
		}
		n := int(binary.BigEndian.Uint32(body[4:8]))
		if n > 65536 || n > len(body)-8 {
			return nil, errProtocol
		}
		id := uint32(body[1])<<16 | uint32(body[2])<<8 | uint32(body[3])
		p := body[8 : 8+n]
		switch body[0] {
		case 2:
			if id != 1 || n == 0 || len(*data)+n > 8192 {
				return nil, errProtocol
			}
			*data = append(*data, p...)
		case 4:
			if id != 1 || n != 4 || binary.BigEndian.Uint32(p) == 0 {
				return nil, errProtocol
			}
		case 5:
			if id != 0 || n > 64 {
				return nil, errProtocol
			}
			pongs = append(pongs, sharedFrame(6, 0, p)...)
		default:
			return nil, errProtocol
		}
		body = body[8+n:]
	}
	return pongs, nil
}

func responseComplete(data []byte, x *mtExchange) (bool, error) {
	if len(data) < x.consumed {
		return true, errProtocol
	}
	pending := make([]byte, len(data)-x.consumed)
	x.read.XORKeyStream(pending, data[x.consumed:])
	x.response = append(x.response, pending...)
	x.consumed = len(data)
	if len(x.response) < 4 {
		return false, nil
	}
	length := int(binary.LittleEndian.Uint32(x.response[:4]))
	if length < 20 || length > 4096 {
		return true, errProtocol
	}
	if len(x.response) < length+4 {
		return false, nil
	}
	if len(x.response) != length+4 {
		return true, errProtocol
	}
	return true, x.verifyPacket(x.response[4:])
}

func checkHTTPStream(ctx context.Context, client *http.Client, endpoint, token, carrier string, batch []byte, x *mtExchange) error {
	seq := 1
	cursor := "0"
	var data []byte
	headers := func() map[string]string {
		h := map[string]string{}
		if carrier == "https-lanes" {
			h["X-Lane-ID"] = "1"
		}
		return h
	}
	send := func(b []byte) error {
		h := headers()
		h["X-Up-Seq"] = strconv.Itoa(seq)
		r, e := webRequest(ctx, client, "POST", endpoint+"up", token, b, h)
		if e != nil {
			return e
		}
		_, e = webBody(r, 64)
		if e != nil || r.StatusCode != 204 || r.Header.Get("X-Up-Ack") != strconv.Itoa(seq) {
			return errProtocol
		}
		seq++
		return nil
	}
	if e := send(batch); e != nil {
		return e
	}
	for {
		h := headers()
		h["X-Down-Cursor"] = cursor
		r, e := webRequest(ctx, client, "POST", endpoint+"down", token, nil, h)
		if e != nil {
			return e
		}
		body, e := webBody(r, 65536)
		if e != nil {
			return e
		}
		next := r.Header.Get("X-Down-Cursor")
		v, e := strconv.ParseUint(next, 10, 64)
		old, _ := strconv.ParseUint(cursor, 10, 64)
		if e != nil || strconv.FormatUint(v, 10) != next {
			return errProtocol
		}
		if r.StatusCode == 204 {
			if len(body) != 0 || v != old {
				return errProtocol
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		if r.StatusCode != 200 || v != old+1 {
			return errProtocol
		}
		cursor = next
		pongs, e := acceptSharedFrames(body, &data)
		if e != nil {
			return e
		}
		if len(pongs) > 0 {
			if carrier == "https-lanes" {
				return errProtocol
			}
			if e = send(pongs); e != nil {
				return e
			}
		}
		if done, e := responseComplete(data, x); done {
			return e
		}
	}
}

func checkWebSocketStream(ctx context.Context, client *http.Client, origin, endpoint, token, carrier string, batch []byte, x *mtExchange) error {
	location := strings.Replace(endpoint+"ws", "https:", "wss:", 1)
	config, e := websocket.NewConfig(location, origin)
	if e != nil {
		return errProtocol
	}
	protocol := "tproxy-v1." + token
	if carrier == "websocket-lanes" {
		protocol = "tproxy-lane-v1." + token + ".1"
	}
	config.Protocol = []string{protocol}
	var tlsConfig *tls.Config
	if transport, ok := client.Transport.(*http.Transport); ok {
		tlsConfig = transport.TLSClientConfig
	}
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	config.TlsConfig = tlsConfig.Clone()
	u, _ := url.Parse(location)
	raw, e := (&tls.Dialer{Config: config.TlsConfig}).DialContext(ctx, "tcp", webSocketAddress(u))
	if e != nil {
		return e
	}
	defer func() { _ = raw.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if e = raw.SetDeadline(deadline); e != nil {
			return e
		}
	}
	ws, e := websocket.NewClient(config, raw)
	if e != nil {
		return e
	}
	defer func() { _ = ws.Close() }()
	ws.MaxPayloadBytes = 65536
	if len(ws.Config().Protocol) != 1 || ws.Config().Protocol[0] != protocol {
		return errProtocol
	}
	if e = websocket.Message.Send(ws, batch); e != nil {
		return e
	}
	var data []byte
	for {
		var body []byte
		if e = websocket.Message.Receive(ws, &body); e != nil {
			return e
		}
		pongs, e := acceptSharedFrames(body, &data)
		if e != nil {
			return e
		}
		if len(pongs) > 0 {
			if carrier == "websocket-lanes" {
				return errProtocol
			}
			if e = websocket.Message.Send(ws, pongs); e != nil {
				return e
			}
		}
		if done, e := responseComplete(data, x); done {
			return e
		}
	}
}

func webSocketAddress(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}
