package agent

import "context"

// startToolProgress attaches a small, lossy queue to the tool operation.
// Observation can block independently of JavaScript and the tool's cleanup.
// Join after cancelling the operation, before publishing its finished event.
func startToolProgress(ctx context.Context, emit func(stream, text string)) (report func(stream, text string), wait func()) {
	type sample struct{ stream, text string }
	queue := make(chan sample, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case line := <-queue:
				if ctx.Err() != nil {
					return
				}
				emit(line.stream, line.text)
			}
		}
	}()
	return func(stream, text string) {
		select {
		case queue <- sample{stream, text}:
		default:
		}
	}, func() { <-done }
}
