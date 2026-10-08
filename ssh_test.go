package main

import (
	"bytes"
	"context"
	"errors"
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

func TestTerminalPromptCompatibility(t *testing.T) {
	for _, raw := range []string{
		"Copyright H3C\r\n<master_S10508>\r\n",
		"\x1b[32m<master_S10508>\x1b[0m\x00\a",
		"<master_S10508>\x1b[K",
		"Loading the switch configuration...\r<master_S10508>\x1b[K",
		"<master_S10508>\x1b]0;Switch\a",
		"[master_S10508-GigabitEthernet1/0/1]\x1b[0m",
		"<master_S10509\b8>",
	} {
		t.Run(raw, func(t *testing.T) {
			var b lockedBuffer
			b.Write([]byte(raw))
			if err := waitShellOutput(&b, 0, time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPromptMustBeWholeLine(t *testing.T) {
	for _, raw := range []string{
		"<SW>display mac-address aabb-ccdd-eeff",
		"[SW]display version", "Save? [Y/N]:", "[Y/N]", "<SW>\x1b[",
		"<SW>\r\nStill working", "[some status message]",
	} {
		var b lockedBuffer
		b.Write([]byte(raw))
		if err := waitShellOutput(&b, 0, 450*time.Millisecond); err == nil {
			t.Fatalf("accepted non-prompt %q", raw)
		}
	}
}

func TestSplitEscapeAndFreshCommandOutput(t *testing.T) {
	var b lockedBuffer
	b.Write([]byte("<old>"))
	start := b.Len()
	b.Write([]byte("display version\r\n<master_S10508>\x1b["))
	go func() { time.Sleep(550 * time.Millisecond); b.Write([]byte("0m")) }()
	started := time.Now()
	if err := waitShellOutput(&b, start, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 550*time.Millisecond {
		t.Fatal("accepted incomplete escape or stale prompt")
	}
}

func TestCancellationStopsPromptWaitAndSaveReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var b lockedBuffer
	var sent bytes.Buffer
	b.Write([]byte("Save? [Y/N]:"))
	cancel()
	started := time.Now()
	err := waitCommandOutputContext(ctx, &b, 0, time.Minute, &sent, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if sent.Len() != 0 {
		t.Fatal("sent confirmation after cancellation")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancellation was not prompt")
	}
}

func TestCancellationInterruptsActivePromptWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var b lockedBuffer
	done := make(chan error, 1)
	go func() { done <- waitCommandOutputContext(ctx, &b, 0, time.Minute, nil, false) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not stop")
	}
}
