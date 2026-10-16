package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// Complete returns identifier candidates for a bare identifier or dotted
// property chain. Property discovery runs inside the engine so that the
// captured reflection intrinsics can inspect proxies under ctx without calling
// getters.
func (s *Session) Complete(ctx context.Context, token string) (candidates []string, err error) {
	h := s.h
	defer func() {
		if h.exitRequested {
			candidates = nil
			err = &TerminationError{Code: h.exitCode}
			return
		}
		if sessionFatal(err) {
			candidates = nil
			err = &TerminationError{Code: 1, Cause: err}
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	path, prefix, qualified, ok := completionPath(token)
	if !ok {
		return nil, nil
	}
	if path == nil {
		path = []string{}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	inputCtx, cancel := context.WithCancel(ctx)
	endOperation := h.beginAgentOperation(inputCtx)
	defer endOperation()
	generation := h.loop.resourceGeneration
	value, callErr := h.completion.CallContext(inputCtx, path, len(path) > 0 && hasString(s.lexicalNames, path[0]))
	newStdlibWork := h.loop.std.Pending() || h.loop.hasResourcesSince(generation)
	needsCleanup := callErr != nil || ctx.Err() != nil || h.rt.Busy() || newStdlibWork || len(h.loop.timers) != 0 || len(h.unhandled) != 0
	cancel()

	// Proxy traps can schedule Promise reactions or host work while reflection
	// runs. Once completion returns, cancel those effects and discard queued JS
	// before another input can observe them.
	var cleanupErr error
	if needsCleanup {
		cleanupErr = s.cleanupCancelledOperation(inputCtx, newStdlibWork)
	}
	if cleanupErr != nil && sessionFatal(cleanupErr) {
		return nil, cleanupErr
	}
	if ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), cleanupErr)
	}
	if callErr != nil {
		if sessionFatal(callErr) {
			return nil, errors.Join(callErr, cleanupErr)
		}
		// A throwing Proxy trap makes this completion attempt unproductive;
		// it does not turn a recoverable editing operation into an Eval error.
		return nil, nil
	}
	if cleanupErr != nil {
		return nil, cleanupErr
	}

	keys := make([]string, 0, value.Len())
	keyCount := value.Len()
	for i := 0; i < keyCount; i++ {
		key, err := value.Index(i)
		if err != nil {
			return nil, err
		}
		if key.IsString() {
			name := key.String()
			if validIdentifier(name) && strings.HasPrefix(name, prefix) {
				keys = append(keys, name)
			}
		}
	}
	if !qualified {
		for name := range s.lexicalNames {
			if strings.HasPrefix(name, prefix) {
				keys = append(keys, name)
			}
		}
		for _, keyword := range completionKeywords {
			if strings.HasPrefix(keyword, prefix) {
				keys = append(keys, keyword)
			}
		}
	}

	base := ""
	if qualified {
		parts := strings.Split(token, ".")
		base = strings.Join(parts[:len(parts)-1], ".") + "."
	}
	for i := range keys {
		keys[i] = base + keys[i]
	}
	sort.Strings(keys)
	return deduplicateStrings(keys), nil
}

func (s *Session) cleanupCancelledOperation(ctx context.Context, newStdlibWork bool) error {
	h := s.h
	if err := h.discardCancelledJobs(); err != nil {
		return errors.Join(err, h.takeUnhandled())
	}
	err := h.loop.stopTimers(ctx)
	if newStdlibWork || h.loop.std.Context().Err() != nil {
		h.rt.Close()
		return errors.Join(err, fmt.Errorf("active stdlib work requires ending the REPL session: %w", quickjs.ErrClosed))
	}
	err = errors.Join(err, h.drainCancelled(ctx))
	return errors.Join(err, h.takeUnhandled())
}

func completionPath(token string) (path []string, prefix string, qualified bool, ok bool) {
	if token == "" {
		return nil, "", false, true
	}
	parts := strings.Split(token, ".")
	qualified = len(parts) > 1
	if qualified {
		for _, part := range parts[:len(parts)-1] {
			if !validIdentifier(part) {
				return nil, "", false, false
			}
		}
		prefix = parts[len(parts)-1]
		if prefix != "" && !validIdentifier(prefix) {
			return nil, "", false, false
		}
		return parts[:len(parts)-1], prefix, true, true
	}
	if !validIdentifier(token) {
		return nil, "", false, false
	}
	return nil, token, false, true
}

func validIdentifier(name string) bool {
	if name == "" {
		return false
	}
	first := true
	for _, r := range name {
		if first {
			if !identifierStart(r) {
				return false
			}
			first = false
		} else if !identifierContinue(r) {
			return false
		}
	}
	return true
}

func identifierStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '$'
}

func hasString(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func deduplicateStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

var completionKeywords = []string{
	"await", "break", "case", "catch", "class", "const", "continue", "debugger",
	"default", "delete", "do", "else", "enum", "export", "extends", "false",
	"finally", "for", "function", "if", "import", "in", "instanceof", "let", "new",
	"null", "return", "super", "switch", "this", "throw", "true", "try", "typeof",
	"var", "void", "while", "with", "yield",
	// Contextual keywords accepted by the pinned JavaScript parser in common
	// syntax positions.
	"as", "async", "from", "get", "of", "set", "static",
}

// completionHelperSource captures all JavaScript reflection operations before
// user input can replace them. It returns property names rather than reading
// final properties, so getters are never called. Accessor properties in an
// intermediate chain are intentionally not traversed.
const completionHelperSource = `(() => {
	const global = globalThis;
	const ObjectCtor = Object;
	const FunctionCtor = Function;
	const ownNames = Object.getOwnPropertyNames;
	const ownDescriptor = Object.getOwnPropertyDescriptor;
	const getPrototype = Object.getPrototypeOf;
	const create = Object.create;

	function descriptorInChain(target, key) {
		let current = target;
		while (current !== null) {
			const descriptor = ownDescriptor(current, key);
			if (descriptor !== undefined) return descriptor;
			current = getPrototype(current);
		}
		return undefined;
	}

	function dataValue(descriptor) {
		if (descriptor === undefined) return undefined;
		const valueDescriptor = ownDescriptor(descriptor, "value");
		return valueDescriptor === undefined ? undefined : valueDescriptor.value;
	}

	return function(path, lexicalRoot) {
		try {
			let target = global;
			if (path.length !== 0) {
				if (lexicalRoot) {
					// The identifier was validated in Go. A Function body has only
					// the global environment, so helper locals cannot shadow it.
					target = FunctionCtor("return (" + path[0] + ")")();
				} else {
					target = dataValue(descriptorInChain(global, path[0]));
				}
				for (let i = 1; i < path.length; i++) {
					if (target === null || target === undefined) return create(null);
					target = dataValue(descriptorInChain(ObjectCtor(target), path[i]));
				}
				if (target === null || target === undefined) return create(null);
				target = ObjectCtor(target);
			}

			const result = create(null);
			let count = 0;
			let current = target;
			while (current !== null) {
				const names = ownNames(current);
				for (let i = 0; i < names.length; i++) result[count++] = names[i];
				current = getPrototype(current);
			}
			result.length = count;
			return result;
		} catch (_) {
			return create(null);
		}
	};
})()`
