package protocolprobe

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"time"
)

func hmacSHA(secret, b []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(b)
	return h.Sum(nil)
}

func tlsRecord(typ byte, p []byte) []byte {
	return append([]byte{typ, 3, 3, byte(len(p) >> 8), byte(len(p))}, p...)
}

func clientHello(sni string, secret []byte) ([]byte, error) {
	key, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	body := []byte{3, 3}
	body = append(body, make([]byte, 32)...)
	session := make([]byte, 32)
	if _, e = rand.Read(session); e != nil {
		return nil, e
	}
	body = append(body, 32)
	body = append(body, session...)
	body = append(body, 0, 6, 0x13, 1, 0x13, 2, 0x13, 3, 1, 0)
	var extensions []byte
	ext := func(typ uint16, data []byte) {
		extensions = append(extensions, byte(typ>>8), byte(typ), byte(len(data)>>8), byte(len(data)))
		extensions = append(extensions, data...)
	}
	sn := append([]byte{byte((len(sni) + 3) >> 8), byte(len(sni) + 3), 0, byte(len(sni) >> 8), byte(len(sni))}, []byte(sni)...)
	ext(0, sn)
	ext(43, []byte{2, 3, 4})
	ext(10, []byte{0, 2, 0, 29})
	ext(13, []byte{0, 6, 4, 3, 8, 4, 8, 7})
	ext(51, append([]byte{0, 36, 0, 29, 0, 32}, key.PublicKey().Bytes()...))
	body = append(body, byte(len(extensions)>>8), byte(len(extensions)))
	body = append(body, extensions...)
	hs := append([]byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	hello := tlsRecord(22, hs)
	hello[1] = 3
	hello[2] = 1
	digest := hmacSHA(secret, hello)
	stamp := make([]byte, 4)
	binary.LittleEndian.PutUint32(stamp, uint32(time.Now().Unix()))
	for i := 0; i < 4; i++ {
		digest[28+i] ^= stamp[i]
	}
	copy(hello[11:43], digest)
	return hello, nil
}

func readTLSRecord(r io.Reader) ([]byte, error) {
	h := make([]byte, 5)
	if _, e := io.ReadFull(r, h); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(h[3:]))
	if h[1] != 3 || h[2] != 3 || n == 0 || n > 18432 {
		return nil, errProtocol
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		return nil, e
	}
	return append(h, b...), nil
}

func dialFakeTLS(ctx context.Context, host string, t Transport, secret []byte) (*fakeTLSConn, error) {
	port := t.Port
	if port == 0 {
		port = 443
	}
	raw, e := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			_ = raw.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	if deadline, ok := ctx.Deadline(); ok {
		if e = raw.SetDeadline(deadline); e != nil {
			stop()
			return nil, e
		}
	}
	hello, e := clientHello(t.SNI, secret)
	if e != nil {
		stop()
		return nil, e
	}
	if _, e = raw.Write(hello); e != nil {
		stop()
		return nil, e
	}
	var flight []byte
	var digest []byte
	for i := 0; i < 7; i++ {
		record, e := readTLSRecord(raw)
		if e != nil {
			stop()
			return nil, e
		}
		if i == 0 {
			if record[0] != 22 || len(record) < 43 || record[5] != 2 {
				stop()
				return nil, errProtocol
			}
			digest = append([]byte{}, record[11:43]...)
			clear(record[11:43])
		} else if i == 1 {
			if record[0] != 20 || len(record) != 6 || record[5] != 1 {
				stop()
				return nil, errProtocol
			}
		} else if record[0] != 23 {
			stop()
			return nil, errProtocol
		}
		flight = append(flight, record...)
		if i >= 2 && hmac.Equal(digest, hmacSHA(secret, append(append([]byte{}, hello[11:43]...), flight...))) {
			success = true
			return &fakeTLSConn{Conn: raw, stop: stop}, nil
		}
	}
	stop()
	return nil, errProtocol
}

type fakeTLSConn struct {
	net.Conn
	pending []byte
	sentCCS bool
	stop    func() bool
}

func (c *fakeTLSConn) Close() error { c.stop(); return c.Conn.Close() }
func (c *fakeTLSConn) Write(p []byte) (int, error) {
	original := len(p)
	if !c.sentCCS {
		if _, e := c.Conn.Write(tlsRecord(20, []byte{1})); e != nil {
			return 0, e
		}
		c.sentCCS = true
	}
	for len(p) > 0 {
		n := len(p)
		if n > 16384 {
			n = 16384
		}
		if _, e := c.Conn.Write(tlsRecord(23, p[:n])); e != nil {
			return 0, e
		}
		p = p[n:]
	}
	return original, nil
}

func (c *fakeTLSConn) Read(p []byte) (int, error) {
	for len(c.pending) == 0 {
		record, e := readTLSRecord(c.Conn)
		if e != nil {
			return 0, e
		}
		if record[0] != 23 {
			return 0, errProtocol
		}
		c.pending = record[5:]
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}
