//go:build !linux

package probe

import (
	"context"
	"errors"
	"net"
	"time"
)

// PathMTU is only written for Linux so far.
func PathMTU(ctx context.Context, dst, src net.IP, maxTTL int, timeout time.Duration, progress func(string)) (MTUResult, error) {
	return MTUResult{}, errors.New("--mtu works on Linux only for now")
}
