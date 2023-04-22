package pglocker

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// panicSafeWrapper is a panic safe function wrapper
// that returns panic as error.
func panicSafeWrapper(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			const size = 64 << 10 // 64 KiB.
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]

			err = fmt.Errorf("panic: %s\n\n%s", r, buf) //nolint:goerr113 // Error wrapping is not required for corner case.
		}
	}()

	return f()
}

// withTimeout executes X using detached context with timeout.
func withTimeout(timeout time.Duration, f func(context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	f(ctx)
}
