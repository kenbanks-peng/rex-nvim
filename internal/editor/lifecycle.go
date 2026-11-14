package editor

import (
	"fmt"
	"time"
)

// Detach is best effort: a child blocked in a synchronous operation must not
// prevent renderer cleanup. The caller closes RPC after this returns.
func detachUI(detach func() error, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- detach() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out after %s; closing owned child", timeout)
	}
}
