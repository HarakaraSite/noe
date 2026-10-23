package quickjs

import (
	"errors"

	"forge.harakara.site/littleisland/noe/third_party/go-quickjs/internal/sharedmem"
	"forge.harakara.site/littleisland/noe/third_party/go-quickjs/internal/vm"
)

// Agents share memory through sharedmem, which is filled in here.
func init() {
	sharedmem.Share = func(v any) (any, bool, error) {
		val, ok := v.(Value)
		if !ok || val.rt == nil {
			return nil, false, nil
		}
		m, ok, err := val.rt.SharedMemoryOf(val.v)
		if !ok {
			return nil, false, nil
		}
		return m, true, err
	}
	sharedmem.Attach = func(rt, mem any) (any, error) {
		r := rt.(*Runtime)
		if r.closed {
			return Value{}, ErrClosed
		}
		m, ok := mem.(*vm.SharedMemory)
		if !ok {
			return Value{}, errors.New("quickjs: not shared memory")
		}
		return Value{v: r.rt.NewSharedArrayBuffer(m), rt: r.rt}, nil
	}
}
