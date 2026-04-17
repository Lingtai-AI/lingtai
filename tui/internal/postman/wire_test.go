package postman

import (
	"bytes"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	original := []byte(`{"from":"human","to":"agent_b","message":"hello"}`)

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if !bytes.HasPrefix(encoded, []byte("LTPM")) {
		t.Fatalf("missing LTPM magic, got %q", encoded[:4])
	}

	if encoded[4] != 0x01 {
		t.Errorf("flags = %d, want 1 (zstd)", encoded[4])
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if !bytes.Equal(decoded, original) {
		t.Errorf("roundtrip mismatch:\n got: %s\nwant: %s", decoded, original)
	}
}

func TestDecode_BadMagic(t *testing.T) {
	_, err := Decode([]byte("XXXX\x01data"))
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestDecode_TooShort(t *testing.T) {
	_, err := Decode([]byte("LTP"))
	if err == nil {
		t.Fatal("expected error for short datagram")
	}
}

func TestDecode_UnknownFlags(t *testing.T) {
	_, err := Decode([]byte("LTPM\x99data"))
	if err == nil {
		t.Fatal("expected error for unknown flags")
	}
}

func TestEncode_Compresses(t *testing.T) {
	msg := bytes.Repeat([]byte(`{"key":"value","key":"value"},`), 100)
	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) >= len(msg) {
		t.Errorf("encoded %d bytes >= original %d bytes", len(encoded), len(msg))
	}
}
