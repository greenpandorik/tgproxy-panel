package protocolprobe

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

const testSecret = "000102030405060708090a0b0c0d0e0f"

// This peer independently implements the upstream server boundary, including
// secret-dependent AES keys; it cannot answer a request encrypted with a wrong key.
func testReply(input []byte, mode string) ([]byte, error) {
	if len(input) < 108 {
		return nil, fmt.Errorf("short request")
	}
	secret, _ := hex.DecodeString(testSecret)
	key := sha256.Sum256(append(append([]byte{}, input[8:40]...), secret...))
	block, _ := aes.NewCipher(key[:])
	decrypt := cipher.NewCTR(block, input[40:56])
	plain := make([]byte, len(input))
	decrypt.XORKeyStream(plain, input)
	if (!bytes.Equal(plain[56:60], []byte{0xdd, 0xdd, 0xdd, 0xdd}) && !bytes.Equal(plain[56:60], []byte{0xee, 0xee, 0xee, 0xee})) || binary.LittleEndian.Uint16(plain[60:62]) != 2 || binary.LittleEndian.Uint32(plain[64:68]) != 40 || binary.LittleEndian.Uint32(plain[88:92]) != 0xbe7e8ef1 {
		return nil, fmt.Errorf("invalid authenticated request")
	}
	nonce := plain[92:108]
	body := make([]byte, 64)
	binary.LittleEndian.PutUint32(body, 0x05162463)
	copy(body[4:20], nonce)
	for i := 20; i < 36; i++ {
		body[i] = byte(i)
	}
	body[36] = 8
	copy(body[37:45], []byte{0x17, 0xed, 0x48, 0x94, 0x1a, 0x08, 0xf9, 0x81})
	binary.LittleEndian.PutUint32(body[48:52], 0x1cb5c415)
	binary.LittleEndian.PutUint32(body[52:56], 1)
	copy(body[56:], []byte{0x21, 0xc1, 0xae, 0x1c, 0xb1, 0xbd, 0x41, 0xc3})
	if mode == "nonce" {
		body[4] ^= 1
	}
	if mode == "malformed" {
		binary.LittleEndian.PutUint32(body[52:56], 99)
	}
	packet := make([]byte, 88)
	binary.LittleEndian.PutUint32(packet, 84)
	binary.LittleEndian.PutUint64(packet[12:20], uint64(time.Now().Unix())<<32|1)
	binary.LittleEndian.PutUint32(packet[20:24], 64)
	copy(packet[24:], body)
	reverse := append([]byte{}, input[:64]...)
	for i, j := 0, 63; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	key = sha256.Sum256(append(append([]byte{}, reverse[8:40]...), secret...))
	block, _ = aes.NewCipher(key[:])
	cipher.NewCTR(block, reverse[40:56]).XORKeyStream(packet, packet)
	return packet, nil
}

func readRecord(c io.Reader) ([]byte, error) {
	h := make([]byte, 5)
	if _, e := io.ReadFull(c, h); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(h[3:]))
	if n > 18432 {
		return nil, fmt.Errorf("large record")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(c, b)
	return append(h, b...), e
}

func testTLSRecord(typ byte, b []byte) []byte {
	h := []byte{typ, 3, 3, byte(len(b) >> 8), byte(len(b))}
	return append(h, b...)
}

func fakePeer(t *testing.T, mode string) int {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer func() { _ = c.Close() }()
		if e = c.SetDeadline(time.Now().Add(2 * time.Second)); e != nil {
			return
		}
		hello, e := readRecord(c)
		if e != nil {
			return
		}
		if mode == "timeout" {
			_, _ = io.Copy(io.Discard, c)
			return
		}
		secret, _ := hex.DecodeString(testSecret)
		zero := append([]byte{}, hello...)
		clear(zero[11:43])
		mac := hmac.New(sha256.New, secret)
		mac.Write(zero)
		if !hmac.Equal(hello[11:39], mac.Sum(nil)[:28]) {
			return
		}
		server := make([]byte, 80)
		server[0] = 2
		flight := testTLSRecord(22, server)
		flight = append(flight, testTLSRecord(20, []byte{1})...)
		flight = append(flight, testTLSRecord(23, make([]byte, 64))...)
		mac.Reset()
		mac.Write(hello[11:43])
		mac.Write(flight)
		copy(flight[11:43], mac.Sum(nil))
		if mode == "impostor" {
			clear(flight[11:43])
		}
		if _, e = c.Write(flight); e != nil {
			return
		}
		var input []byte
		for len(input) < 108 {
			record, e := readRecord(c)
			if e != nil {
				return
			}
			if record[0] == 23 {
				input = append(input, record[5:]...)
			}
		}
		reply, e := testReply(input, mode)
		if e == nil {
			if _, e = c.Write(testTLSRecord(23, reply)); e != nil {
				return
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestFakeTLSRequiresAuthenticatedTelegramReply(t *testing.T) {
	for _, mode := range []string{"success", "nonce", "malformed", "impostor", "timeout", "wrong-key"} {
		t.Run(mode, func(t *testing.T) {
			port := fakePeer(t, mode)
			secret := testSecret
			if mode == "wrong-key" {
				secret = strings.Repeat("ff", 16)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			result := Check(ctx, "127.0.0.1", Config{FakeTLS: &Transport{Secret: secret, SNI: "cover.example.com", Port: port}})
			want := "failed"
			if mode == "success" {
				want = "ok"
			}
			if result.FakeTLS.Status != want {
				t.Fatalf("%+v want %s", result, want)
			}
		})
	}
}

func TestWEBRequiresAuthenticatedTelegramReply(t *testing.T) {
	for _, mode := range []string{"success", "nonce", "malformed", "impostor", "wrong-key", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var reply []byte
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/":
					if mode == "impostor" {
						if _, e := io.WriteString(w, "cover website"); e != nil {
							return
						}
						return
					}
					if r.URL.Query().Get("bridge") != "sh6fXDq9eFLAGlge4LZMCg4CKsXnMEgAPvYzvhWaiaI" {
						http.NotFound(w, r)
						return
					}
					if _, e := io.WriteString(w, `let bootstrap="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";`); e != nil {
						return
					}
				case "/api/v1/session":
					if r.Header.Get("Authorization") != "Bearer AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("X-Session-Token", "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
					w.Header().Set("X-Down-Cursor", "0")
					w.Header().Set("X-Carrier-Mode", "https")
					if _, e := w.Write([]byte{0x11, 0, 0, 0, 0, 0, 0, 0}); e != nil {
						return
					}
				case "/api/v1/up":
					body, _ := io.ReadAll(r.Body)
					if len(body) > 16 {
						var e error
						reply, e = testReply(body[16:], mode)
						if e != nil {
							http.Error(w, "bad key", 400)
							return
						}
					}
					w.Header().Set("X-Up-Ack", r.Header.Get("X-Up-Seq"))
					w.WriteHeader(204)
				case "/api/v1/down":
					if mode == "timeout" {
						<-r.Context().Done()
						return
					}
					if len(reply) == 0 {
						http.Error(w, "no data", 400)
						return
					}
					w.Header().Set("X-Down-Cursor", "1")
					body := []byte{2, 0, 0, 1, 0, 0, 0, byte(len(reply))}
					if _, e := w.Write(append(body, reply...)); e != nil {
						return
					}
				case "/api/v1/session/unused":
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			// Use the server's actual hostname for capability derivation and its test CA.
			host := "127.0.0.1"
			port, _ := strconv.Atoi(strings.TrimPrefix(server.URL, "https://127.0.0.1:"))
			secret := testSecret
			if mode == "wrong-key" {
				secret = strings.Repeat("ff", 16)
			}
			cfg := Config{WEB: &Transport{Secret: secret, Port: port}}
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			result := checkWithTLS(ctx, host, cfg, server.Client().Transport.(*http.Transport).TLSClientConfig)
			want := "failed"
			if mode == "success" {
				want = "ok"
			}
			if result.WEB.Status != want {
				t.Fatalf("%+v want %s", result, want)
			}
		})
	}
}

func checkWithTLS(ctx context.Context, host string, cfg Config, tlsConfig *tls.Config) Result {
	secret, _ := hex.DecodeString(cfg.WEB.Secret)
	x, request, e := newExchange(secret, 2)
	result := Result{}
	result.WEB.Status = "failed"
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	if e == nil && probeWEB(ctx, host, *cfg.WEB, secret, request, x, client) == nil {
		result.WEB.Status = "ok"
	}
	return result
}

func TestWEBReplyCanSpanCarrierBatches(t *testing.T) {
	secret, _ := hex.DecodeString(testSecret)
	x, request, e := newExchange(secret, 2)
	if e != nil {
		t.Fatal(e)
	}
	reply, e := testReply(request, "success")
	if e != nil {
		t.Fatal(e)
	}
	if done, e := responseComplete(reply[:85], x); done || e != nil {
		t.Fatalf("partial reply completed: %v %v", done, e)
	}
	if done, e := responseComplete(reply, x); !done || e != nil {
		t.Fatalf("full reply rejected: %v %v", done, e)
	}
}

func TestKnownWEBCapabilityVectors(t *testing.T) {
	for _, tc := range []struct{ secret, want string }{
		{testSecret, "MHLEY5PmW1GWqJkSrlmJpvJUiLhBH_QKy6yKg8a0JPk"},
		{"dd" + testSecret, "IpJrt3e7sKtzPyoXy6w-Zj6GGEvsvclN66JzQEfPYLA"},
	} {
		secret, _ := hex.DecodeString(tc.secret)
		if got := webCapability("proxy.example.com", "", secret); got != tc.want {
			t.Fatalf("got %s", got)
		}
	}
}

func TestBodylessWEBPollDoesNotSendContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	response, e := webRequest(t.Context(), server.Client(), "POST", server.URL, "", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	_ = response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("bodyless WEB request contains Content-Type")
	}
}

func TestWEBPlainUsesIntermediateTransport(t *testing.T) {
	secret, _ := hex.DecodeString(testSecret)
	_, input, e := newExchange(secret, 2)
	if e != nil {
		t.Fatal(e)
	}
	key := sha256.Sum256(append(append([]byte{}, input[8:40]...), secret...))
	block, _ := aes.NewCipher(key[:])
	plain := make([]byte, len(input))
	cipher.NewCTR(block, input[40:56]).XORKeyStream(plain, input)
	if !bytes.Equal(plain[56:60], []byte{0xee, 0xee, 0xee, 0xee}) {
		t.Fatal("plain WEB used a secure transport tag")
	}
}

func TestWebSocketCarrierExchange(t *testing.T) {
	for _, carrier := range []string{"websocket", "websocket-lanes"} {
		t.Run(carrier, func(t *testing.T) {
			server := httptest.NewTLSServer(websocket.Handler(func(ws *websocket.Conn) {
				defer func() { _ = ws.Close() }()
				var body []byte
				if e := websocket.Message.Receive(ws, &body); e != nil {
					return
				}
				if len(body) < 16 {
					return
				}
				reply, e := testReply(body[16:], "success")
				if e != nil {
					return
				}
				if e = websocket.Message.Send(ws, sharedFrame(2, 1, reply[:85])); e != nil {
					return
				}
				if e = websocket.Message.Send(ws, sharedFrame(2, 1, reply[85:])); e != nil {
					return
				}
			}))
			defer server.Close()
			secret, _ := hex.DecodeString(testSecret)
			x, request, e := newExchange(secret, 2)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if e = checkWebSocketStream(ctx, server.Client(), server.URL, server.URL+"/", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", carrier, append(sharedFrame(1, 1, nil), sharedFrame(2, 1, request)...), x); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestWebSocketAddressDefaultsToHTTPSPort(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"wss://proxy.example.com/api/v1/ws", "proxy.example.com:443"},
		{"wss://proxy.example.com:8443/api/v1/ws", "proxy.example.com:8443"},
		{"wss://[2001:db8::1]/api/v1/ws", "[2001:db8::1]:443"},
	} {
		u, e := url.Parse(tc.input)
		if e != nil {
			t.Fatal(e)
		}
		if got := webSocketAddress(u); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
