package safego

import (
	"go.uber.org/zap"
)

// Go launches a goroutine with panic recovery.
// If the goroutine panics, the panic value is logged and the goroutine exits
// cleanly instead of crashing the process.
//
// Usage:
//
//	safego.Go(logger, "cleanup-loop", func() {
//	    // work that might panic
//	})
func Go(logger *zap.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Goroutine panicked",
					zap.String("goroutine", name),
					zap.Any("panic", r),
					zap.Stack("stack"),
				)
			}
		}()
		fn()
	}()
}
