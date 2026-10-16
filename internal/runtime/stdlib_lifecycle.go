package runtime

import (
	"context"
	"errors"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// stopTimers cancels upstream timers on the owner without closing the loop.
func (l *eventLoop) stopTimers(ctx context.Context) error {
	if len(l.timers) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		if err := l.h.discardCancelledJobs(); err != nil {
			return err
		}
	}
	var result error
	for id := range l.timers {
		_, err := l.clearTimer.CallContext(context.Background(), id)
		result = errors.Join(result, err)
		delete(l.timers, id)
	}
	return result
}

// Unreferenced workers and ports can still deliver callbacks. Track their
// constructors, including the module exports, rather than relying on Pending.
func (l *eventLoop) trackResources() error {
	factory, err := l.h.rt.EvalFile("<noe-stdlib-resources>", `((created, released) => {
  const apply = Reflect.apply, construct = Reflect.construct;
  const wrappers = new WeakMap();
  function resource(obj, method, event, lifecycle) {
   let live = true;
   const id = created();
   const release = () => { if (live) { live = false; released(id); } };
   const original = obj[method];
   if (typeof original === "function") obj[method] = function(...args) {
    const result = apply(original, this, args);
    if (lifecycle) return result;
    if (result && typeof result.then === "function") result.then(release, release);
    else release();
    return result;
   };
   if (lifecycle) {
    // v0.25.3 dispatches Worker lifecycle notifications through this
    // symbol. Web Worker has no public event for natural completion.
    const dispatch = obj[lifecycle];
    Object.defineProperty(obj, lifecycle, {value: function(type, ...args) {
     if (type === "exit") release();
     return apply(dispatch, this, [type, ...args]);
    }});
   } else if (event && typeof obj.addEventListener === "function") {
    obj.addEventListener(event, release, {once: true});
   }
   return obj;
  }
  function wrap(C, kind) {
   if (typeof C !== "function") return C;
   if (wrappers.has(C)) return wrappers.get(C);
   const lifecycle = kind === "worker" ? Object.getOwnPropertySymbols(C.prototype).find(
    key => key.description === "event" && typeof C.prototype[key] === "function") : undefined;
   const wrapped = new Proxy(C, {construct(target, args, newTarget) {
    const obj = construct(target, args, newTarget);
    if (kind === "channel") {
     resource(obj.port1, "close", "close"); resource(obj.port2, "close", "close");
     return obj;
    }
    return resource(obj, kind === "worker" ? "terminate" : "close", kind === "worker" ? "exit" : undefined, lifecycle);
   }});
   wrappers.set(C, wrapped);
   Object.defineProperty(C.prototype, "constructor", {value: wrapped, writable: true, configurable: true});
   return wrapped;
  }
  for (const [name, kind] of [["Worker", "worker"], ["MessageChannel", "channel"], ["BroadcastChannel", "broadcast"]]) {
   if (typeof globalThis[name] === "function") globalThis[name] = wrap(globalThis[name], kind);
  }
  return wrap;
 })`)
	if err != nil {
		return err
	}
	wrap, err := factory.Call(func() uint64 {
		l.resourceGeneration++
		l.resources[l.resourceGeneration] = struct{}{}
		return l.resourceGeneration
	}, func(id uint64) { delete(l.resources, id) })
	if err != nil {
		return err
	}
	exports, err := l.h.rt.RequireModule("node:worker_threads", "")
	if err != nil {
		return err
	}
	values := make(map[string]quickjs.Value)
	for _, key := range exports.Keys() {
		value, err := exports.Get(key)
		if err != nil {
			return err
		}
		values[key] = value
	}
	for name, kind := range map[string]string{"Worker": "worker", "MessageChannel": "channel", "BroadcastChannel": "broadcast"} {
		value, ok := values[name]
		if !ok {
			continue
		}
		wrapped, err := wrap.Call(value, kind)
		if err != nil {
			return err
		}
		values[name] = wrapped
		if def, ok := values["default"]; ok && def.IsObject() {
			if err := def.Set(name, wrapped); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"worker_threads", "node:worker_threads"} {
		if err := l.h.rt.SetModuleValues(name, values); err != nil {
			return err
		}
	}
	return nil
}
