package main

import (
	"bytes"
	"testing"
	"time"
)

// SetDefaults silently drops names it does not recognise, so a typo would
// quietly remove a legacy algorithm. Make sure every configured name survives.
func TestSSHClientConfigKeepsLegacyAlgorithms(t *testing.T) {
	config := newSSHClientConfig("admin", "admin", time.Second)
	config.SetDefaults()
	if len(config.KeyExchanges) != len(sshKeyExchanges) {
		t.Fatalf("KeyExchanges = %v, want %v", config.KeyExchanges, sshKeyExchanges)
	}
	if len(config.Ciphers) != len(sshCiphers) {
		t.Fatalf("Ciphers = %v, want %v", config.Ciphers, sshCiphers)
	}
}

func TestShellDoesNotFinishOnEchoOrDelayedOutput(t *testing.T) {
	var b lockedBuffer
	b.Write([]byte("display mac-address aabb-ccdd-eeff\r\n"))
	go func() { time.Sleep(1200 * time.Millisecond); b.Write([]byte("aabb-ccdd-eeff 9 GE1/0/1\r\n<SW>")) }()
	started := time.Now()
	if err := waitShellOutput(&b, 0, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 1200*time.Millisecond {
		t.Fatal("returned on echo before prompt")
	}
}

func TestSaveConfirmation(t *testing.T) {
	var b lockedBuffer
	var sent bytes.Buffer
	b.Write([]byte("Save? [Y/N]:"))
	go func() {
		time.Sleep(600 * time.Millisecond)
		b.Write([]byte("\r\nSave the configuration successfully.\r\n<SW>"))
	}()
	if err := waitCommandOutput(&b, 0, 2*time.Second, &sent, true); err != nil {
		t.Fatal(err)
	}
	if sent.String() != "y\n" {
		t.Fatalf("unexpected save replies %q", sent.String())
	}
}
