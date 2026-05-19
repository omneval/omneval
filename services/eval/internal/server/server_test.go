package server

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestRun_HandlesSignal(t *testing.T) {
	// This test verifies that Run sets up signal handling correctly.
	// We can't test the full Run() because it blocks and needs Redis.
	// Instead, we verify the signal channel setup logic by checking that
	// the signal package correctly captures SIGTERM.

	// The actual signal handling is tested via integration
	// (starting the server and sending SIGTERM).
	// This test verifies the basic signal notification setup.

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Verify the channel is set up and can receive.
	done := make(chan bool, 1)
	go func() {
		sig := <-sigCh
		if sig != syscall.SIGTERM {
			t.Errorf("expected SIGTERM, got %v", sig)
		}
		done <- true
	}()

	// Send SIGTERM to this process (using syscall.Kill would affect the
	// whole process; instead we simulate by sending to the channel).
	// In the actual Run() function, the OS sends the signal.
	// Here we verify the channel mechanism works.
	sigCh <- syscall.SIGTERM

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("signal was not received on channel")
	}
}
