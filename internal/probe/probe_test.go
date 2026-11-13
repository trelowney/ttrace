package probe

import "testing"

func TestQuotedTOS(t *testing.T) {
	v4 := []byte{0x45, 0xb8, 0, 0}
	if tos, ok := QuotedTOS(v4, false); !ok || tos != 0xb8 {
		t.Errorf("v4: %d %v", tos, ok)
	}
	// IPv6: version 6, traffic class 0xb8 spread over the first two bytes
	v6 := []byte{0x6b, 0x80, 0, 0}
	if tc, ok := QuotedTOS(v6, true); !ok || tc != 0xb8 {
		t.Errorf("v6: %d %v", tc, ok)
	}
	if _, ok := QuotedTOS([]byte{0x45}, false); ok {
		t.Error("short packet accepted")
	}
	if _, ok := QuotedTOS(v4, true); ok {
		t.Error("IPv4 header read as IPv6")
	}
}
