//go:build windows

package sshremote

import "testing"

func TestWindowsDefaultsToOpenSSHNamedPipe(t *testing.T) {
	if defaultAgentEndpoint != windowsOpenSSHAgentPipe {
		t.Fatalf("endpoint=%q", defaultAgentEndpoint)
	}
}
