//go:build windows

package sshremote

import (
	"context"
	"net"
	"strings"

	winio "github.com/Microsoft/go-winio"
)

const windowsOpenSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

func dialAgent(ctx context.Context, endpoint string) (net.Conn, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = windowsOpenSSHAgentPipe
	}
	if strings.HasPrefix(endpoint, `\\.\pipe\`) {
		return winio.DialPipeContext(ctx, endpoint)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint)
}
