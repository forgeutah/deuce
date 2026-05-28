package sshproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// errPermissiveHostKeyMode is returned when an existing host-key file
// has a mode wider than 0600 (e.g., 0644). OpenSSH refuses to use such
// keys; we adopt the same posture.
var errPermissiveHostKeyMode = errors.New("host key file mode is too permissive (must be 0600)")

// errPermissiveHostKeyDir is returned when the parent directory of the
// host-key file has a mode wider than 0700.
var errPermissiveHostKeyDir = errors.New("host key directory mode is too permissive (must be 0700)")

// loadOrGenerateHostKey reads an existing ed25519 host key from path, or
// generates a new one if the file does not exist. Generated keys are
// written atomically with mode 0600; the parent directory is created
// with mode 0700 (regardless of the process's umask).
//
// Refuses to load existing files whose mode is wider than 0600, or whose
// parent directory mode is wider than 0700. Caller is expected to surface
// this as a fatal startup error so operators tighten permissions
// before the proxy ever accepts a connection.
func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	if path == "" {
		return nil, fmt.Errorf("host key path must not be empty")
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		// File exists: validate mode then load.
		if info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("%w: %s has mode %o", errPermissiveHostKeyMode, path, info.Mode().Perm())
		}
		if dirInfo, dirErr := os.Stat(filepath.Dir(path)); dirErr == nil {
			if dirInfo.Mode().Perm()&0o077 != 0 {
				return nil, fmt.Errorf("%w: %s has mode %o", errPermissiveHostKeyDir, filepath.Dir(path), dirInfo.Mode().Perm())
			}
		}
		pemBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read host key: %w", readErr)
		}
		signer, parseErr := ssh.ParsePrivateKey(pemBytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse host key: %w", parseErr)
		}
		return signer, nil

	case errors.Is(err, os.ErrNotExist):
		// Generate a new key.
		return generateHostKey(path)

	default:
		return nil, fmt.Errorf("stat host key: %w", err)
	}
}

func generateHostKey(path string) (ssh.Signer, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create host key dir: %w", err)
	}
	// MkdirAll respects the process umask; tighten explicitly so a loose
	// umask can't produce a 0755 directory.
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod host key dir: %w", err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 generate: %w", err)
	}

	block, err := ssh.MarshalPrivateKey(priv, "deuce-ssh-proxy")
	if err != nil {
		return nil, fmt.Errorf("marshal host key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	// Atomic create-and-write with O_EXCL so concurrent starts cannot
	// race and produce two different host keys.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open host key: %w", err)
	}
	if _, err := f.Write(pemBytes); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write host key: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close host key: %w", err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("signer from key: %w", err)
	}
	return signer, nil
}
