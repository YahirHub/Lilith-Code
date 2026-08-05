//go:build !windows

package sshremote

import (
	"context"
	"net"
)

func dialAgent(ctx context.Context, endpoint string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint)
}
