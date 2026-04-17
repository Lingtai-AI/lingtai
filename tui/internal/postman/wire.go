package postman

import (
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Wire protocol:
//   bytes 0-3: magic "LTPM"
//   byte  4:   flags (0x01 = zstd compressed)
//   bytes 5+:  payload (compressed message.json)

var magic = []byte("LTPM")

const flagZstd byte = 0x01

// Reusable encoder/decoder — created once, goroutine-safe.
var (
	encOnce sync.Once
	decOnce sync.Once
	encInst *zstd.Encoder
	decInst *zstd.Decoder
)

func getEncoder() *zstd.Encoder {
	encOnce.Do(func() {
		encInst, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	})
	return encInst
}

func getDecoder() *zstd.Decoder {
	decOnce.Do(func() {
		decInst, _ = zstd.NewReader(nil)
	})
	return decInst
}

// Encode compresses payload with zstd and prepends the LTPM header.
func Encode(payload []byte) ([]byte, error) {
	enc := getEncoder()
	compressed := enc.EncodeAll(payload, nil)

	buf := make([]byte, 0, 5+len(compressed))
	buf = append(buf, magic...)
	buf = append(buf, flagZstd)
	buf = append(buf, compressed...)
	return buf, nil
}

// Decode verifies the LTPM header and decompresses the payload.
func Decode(data []byte) ([]byte, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("datagram too short: %d bytes", len(data))
	}
	if string(data[:4]) != string(magic) {
		return nil, fmt.Errorf("bad magic: %q", data[:4])
	}
	flags := data[4]
	if flags != flagZstd {
		return nil, fmt.Errorf("unknown flags: 0x%02x", flags)
	}

	dec := getDecoder()
	return dec.DecodeAll(data[5:], nil)
}
