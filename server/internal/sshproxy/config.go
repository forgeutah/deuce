package sshproxy

import (
	"fmt"
	"time"
)

// Config is the resolved configuration for the SSH proxy.
// Constructed by the caller from app-level config and passed to New.
type Config struct {
	// ListenAddr is the network address the proxy listens on (e.g., ":2222").
	// Empty means the SSH listener is disabled.
	ListenAddr string

	// HostKeyPath is where the ed25519 host key is persisted. The parent
	// directory must be 0700 or stricter; the key file must be 0600.
	// Generated on first start if missing.
	HostKeyPath string

	// ServerVersion is the SSH banner string. Must begin with "SSH-2.0-".
	ServerVersion string

	// HandshakeTimeout bounds how long a new TCP connection may take to
	// complete the SSH handshake. Defaults to 10s.
	HandshakeTimeout time.Duration

	// MaxHandshakesPerIP caps the number of concurrent in-progress
	// handshakes per source IP. Defaults to 8.
	MaxHandshakesPerIP int

	// MaxChannelsPerConn caps the number of session channels open at any
	// time on a single SSH connection. Defaults to 8.
	MaxChannelsPerConn int

	// GoroutineCap is the global goroutine threshold above which the
	// accept loop refuses new connections without entering the SSH
	// handshake. runtime.NumGoroutine() is the cheap signal; once it
	// exceeds this cap the next accept disconnects with "server
	// overloaded". Defaults to 4000. Set to a smaller value in tests to
	// simulate overload.
	GoroutineCap int
}

// applyDefaults fills in zero-valued fields with the documented defaults.
func (c *Config) applyDefaults() {
	if c.ServerVersion == "" {
		c.ServerVersion = "SSH-2.0-Deuce"
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.MaxHandshakesPerIP == 0 {
		c.MaxHandshakesPerIP = 8
	}
	if c.MaxChannelsPerConn == 0 {
		c.MaxChannelsPerConn = 8
	}
	if c.GoroutineCap == 0 {
		c.GoroutineCap = 4000
	}
}

// Validate checks for required fields and obviously-wrong values.
func (c *Config) Validate() error {
	if c.HostKeyPath == "" {
		return fmt.Errorf("HostKeyPath is required")
	}
	if c.HandshakeTimeout < 0 {
		return fmt.Errorf("HandshakeTimeout must be non-negative")
	}
	if c.MaxHandshakesPerIP < 1 {
		return fmt.Errorf("MaxHandshakesPerIP must be >= 1")
	}
	if c.MaxChannelsPerConn < 1 {
		return fmt.Errorf("MaxChannelsPerConn must be >= 1")
	}
	if c.GoroutineCap < 1 {
		return fmt.Errorf("GoroutineCap must be >= 1")
	}
	return nil
}
