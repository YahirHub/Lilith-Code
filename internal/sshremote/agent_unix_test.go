//go:build !windows

package sshremote

import "testing"

func TestUnixHasNoImplicitAgentEndpoint(t *testing.T) {
	if defaultAgentEndpoint != "" {
		t.Fatalf("endpoint implícito inesperado: %q", defaultAgentEndpoint)
	}
}
