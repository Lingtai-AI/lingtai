package postman

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

var magic = []byte("LTPM")

const flagZstd byte = 0x01

func Encode(payload []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	defer enc.Close()

	compressed := enc.EncodeAll(payload, nil)

	buf := make([]byte, 0, 5+len(compressed))
	buf = append(buf, magic...)
	buf = append(buf, flagZstd)
	buf = append(buf, compressed...)
	return buf, nil
}

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

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer dec.Close()

	return dec.DecodeAll(data[5:], nil)
}
