package protocolprobe

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

var errProtocol = errors.New("authenticated protocol exchange failed")

type mtExchange struct {
	write, read cipher.Stream
	nonce       [16]byte
	response    []byte
	consumed    int
}

func newExchange(secret []byte, dc int) (*mtExchange, []byte, error) {
	secure := len(secret) == 17
	if secure {
		secret = secret[1:]
	}
	header := make([]byte, 64)
	for {
		if _, e := rand.Read(header); e != nil {
			return nil, nil, e
		}
		prefix := string(header[:4])
		if header[0] != 0xef && header[0] != 0x16 && prefix != "POST" && prefix != "GET " && prefix != "HEAD" && prefix != "\xee\xee\xee\xee" && prefix != "\xdd\xdd\xdd\xdd" && binary.LittleEndian.Uint32(header[4:8]) != 0 {
			break
		}
	}
	for i := 56; i < 60; i++ {
		header[i] = 0xee
		if secure {
			header[i] = 0xdd
		}
	}
	binary.LittleEndian.PutUint16(header[60:62], uint16(dc))
	reversed := append([]byte{}, header...)
	for i, j := 0, 63; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	stream := func(h []byte) cipher.Stream {
		key := sha256.Sum256(append(append([]byte{}, h[8:40]...), secret...))
		b, _ := aes.NewCipher(key[:])
		return cipher.NewCTR(b, h[40:56])
	}
	x := &mtExchange{write: stream(header), read: stream(reversed)}
	if _, e := rand.Read(x.nonce[:]); e != nil {
		return nil, nil, e
	}
	encrypted := make([]byte, 64)
	x.write.XORKeyStream(encrypted, header)
	copy(header[56:], encrypted[56:])
	request := make([]byte, 44)
	binary.LittleEndian.PutUint32(request, 40)
	binary.LittleEndian.PutUint64(request[12:20], uint64(time.Now().UnixNano()/1e9)<<32|uint64(time.Now().Nanosecond())&0xfffffffc)
	binary.LittleEndian.PutUint32(request[20:24], 20)
	binary.LittleEndian.PutUint32(request[24:28], 0xbe7e8ef1)
	copy(request[28:], x.nonce[:])
	x.write.XORKeyStream(request, request)
	return x, append(header, request...), nil
}

func (x *mtExchange) verify(r io.Reader) error {
	decrypted := &cipher.StreamReader{S: x.read, R: r}
	header := make([]byte, 4)
	if _, e := io.ReadFull(decrypted, header); e != nil {
		return e
	}
	length := binary.LittleEndian.Uint32(header)
	if length < 20 || length > 4096 {
		return errProtocol
	}
	packet := make([]byte, length)
	if _, e := io.ReadFull(decrypted, packet); e != nil {
		return e
	}
	return x.verifyPacket(packet)
}

func (x *mtExchange) verifyPacket(packet []byte) error {
	if !bytes.Equal(packet[:8], make([]byte, 8)) {
		return errProtocol
	}
	id := binary.LittleEndian.Uint64(packet[8:16])
	if id&1 == 0 {
		return errProtocol
	}
	n := int(binary.LittleEndian.Uint32(packet[16:20]))
	if n < 56 || n > len(packet)-20 || len(packet)-20-n > 15 {
		return errProtocol
	}
	body := packet[20 : 20+n]
	if binary.LittleEndian.Uint32(body) != 0x05162463 || !bytes.Equal(body[4:20], x.nonce[:]) || bytes.Equal(body[20:36], make([]byte, 16)) {
		return errProtocol
	}
	pqLen := int(body[36])
	if pqLen < 1 || pqLen > 8 || body[37] == 0 {
		return errProtocol
	}
	offset := 36 + (1+pqLen+3)&^3
	if offset+8 > len(body) || binary.LittleEndian.Uint32(body[offset:]) != 0x1cb5c415 {
		return errProtocol
	}
	count := int(binary.LittleEndian.Uint32(body[offset+4:]))
	if count < 1 || count > 64 || offset+8+count*8 != len(body) {
		return errProtocol
	}
	return nil
}

func probe(ctx context.Context, host string, t Transport, dc int, fake bool) error {
	secret, e := hex.DecodeString(t.Secret)
	if e != nil {
		return errProtocol
	}
	mtSecret := secret
	if fake {
		mtSecret = append([]byte{0xdd}, secret...)
	}
	x, request, e := newExchange(mtSecret, dc)
	if e != nil {
		return e
	}
	if fake {
		conn, e := dialFakeTLS(ctx, host, t, secret)
		if e != nil {
			return e
		}
		defer func() { _ = conn.Close() }()
		if _, e = conn.Write(request); e != nil {
			return e
		}
		return x.verify(conn)
	}
	return probeWEB(ctx, host, t, secret, request, x, nil)
}
