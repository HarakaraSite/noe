package runtime

import (
	"context"
	"math"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Bound milliseconds before conversion so duration multiplication cannot
// overflow into a negative, immediately expiring timer.
const maxSleepMilliseconds = (1<<63 - 1) / int64(time.Millisecond)

func (h *host) installSleep(noe quickjs.Value) error {
	wait := func(ms quickjs.Value) (quickjs.Value, error) {
		if ms.Kind() != quickjs.KindNumber {
			return quickjs.Value{}, h.rt.ThrowTypeError("noe.sleep requires a number of milliseconds")
		}
		n := ms.Float()
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < 0 || n > float64(maxSleepMilliseconds) {
			return quickjs.Value{}, h.rt.ThrowRangeError("noe.sleep milliseconds must be an integer from 0 to %d", maxSleepMilliseconds)
		}
		duration := time.Duration(int64(n)) * time.Millisecond
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil, ctx.Err()
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}), nil
	}
	factory, err := h.rt.EvalFile("<noe-sleep>", `(wait => async function sleep(ms) { await wait(ms); })`)
	if err != nil {
		return err
	}
	sleep, err := factory.Call(wait)
	if err != nil {
		return err
	}
	return noe.Set("sleep", sleep)
}
