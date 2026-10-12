package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestSessionCompleteKeywordsGlobalsAndDeclaredBindings(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, input := range []string{
		`let basket = { tomato: 1, toString: 2 }`,
		`let firstDeclared = 1, secondDeclared = 2`,
		`const {nested: nestedValue, shortName} = {nested: 3, shortName: 4}`,
		`const [firstItem, , thirdItem] = [1, 2, 3]`,
		`const {とまと: 果物, 色} = {とまと: 1, 色: 2}`,
		`let 日本語 = { とまと名: 1 }`,
		`if (true) { let blockOnly = 1; }`,
		`function completionFunction() { return 1; }`,
	} {
		result := s.Eval(context.Background(), input)
		if result.Exit || result.Err != nil {
			t.Fatalf("Eval(%q) = %+v", input, result)
		}
	}

	assertCandidates := func(token string, wanted ...string) {
		t.Helper()
		got, err := s.Complete(context.Background(), token)
		if err != nil {
			t.Fatalf("Complete(%q): %v", token, err)
		}
		for _, candidate := range wanted {
			if !containsString(got, candidate) {
				t.Errorf("Complete(%q) = %q, missing %q", token, got, candidate)
			}
		}
		if !isSortedUnique(got) {
			t.Errorf("Complete(%q) is not sorted and unique: %q", token, got)
		}
	}
	assertCandidates("awa", "await")
	assertCandidates("asyn", "async")
	assertCandidates("conso", "console")
	assertCandidates("basket.to", "basket.tomato", "basket.toString")
	assertCandidates("firstDecl", "firstDeclared")
	assertCandidates("secondDecl", "secondDeclared")
	assertCandidates("nestedV", "nestedValue")
	assertCandidates("shortN", "shortName")
	assertCandidates("firstI", "firstItem")
	assertCandidates("thirdI", "thirdItem")
	assertCandidates("果物", "果物")
	assertCandidates("日本語.と", "日本語.とまと名")
	assertCandidates("completionF", "completionFunction")
	assertCandidates("basket.", "basket.tomato", "basket.toString")
}

func TestSessionCompleteDoesNotCallGettersAndUsesCapturedIntrinsics(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, input := range []string{
		`let completionGetterCalls = 0`,
		`let completionObject = { plain: 1, get secret() { completionGetterCalls++; return { safe: 2 }; } }`,
		`Object.defineProperty(globalThis, "completionRootGetter", { configurable: true, get() { completionGetterCalls++; return completionObject; } })`,
	} {
		if result := s.Eval(context.Background(), input); result.Exit || result.Err != nil {
			t.Fatalf("Eval(%q) = %+v", input, result)
		}
	}

	got, err := s.Complete(context.Background(), "completionObject.se")
	if err != nil || !containsString(got, "completionObject.secret") {
		t.Fatalf("accessor property completion = %q, err=%v", got, err)
	}
	got, err = s.Complete(context.Background(), "completionRootGetter.sa")
	if err != nil || len(got) != 0 {
		t.Fatalf("accessor root completion = %q, err=%v", got, err)
	}
	if result := s.Eval(context.Background(), `completionGetterCalls`); result.Exit || result.Err != nil || result.Output != "0" {
		t.Fatalf("completion invoked a getter: %+v", result)
	}

	if result := s.Eval(context.Background(), `let intrinsicTarget = { preserved: 1 }`); result.Exit || result.Err != nil {
		t.Fatalf("target setup = %+v", result)
	}
	if result := s.Eval(context.Background(), `Object.getOwnPropertyNames = () => { throw new Error("replaced names"); }; Object.getOwnPropertyDescriptor = () => { throw new Error("replaced descriptor"); }; Object.getPrototypeOf = () => { throw new Error("replaced prototype"); }; Object.create = () => { throw new Error("replaced create"); }`); result.Exit || result.Err != nil {
		t.Fatalf("intrinsic replacement = %+v", result)
	}
	got, err = s.Complete(context.Background(), "intrinsicTarget.pre")
	if err != nil || !containsString(got, "intrinsicTarget.preserved") {
		t.Fatalf("completion after intrinsic replacement = %q, err=%v", got, err)
	}
}

func TestSessionCompleteProxyFailureAndTimeoutRecover(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, input := range []string{
		`let throwingCompletionProxy = new Proxy({}, { ownKeys() { throw new Error("completion trap"); } })`,
		`let loopingCompletionProxy = new Proxy({}, { ownKeys() { while (true) {} } })`,
	} {
		if result := s.Eval(context.Background(), input); result.Exit || result.Err != nil {
			t.Fatalf("Eval(%q) = %+v", input, result)
		}
	}

	got, err := s.Complete(context.Background(), "throwingCompletionProxy.a")
	if err != nil || len(got) != 0 {
		t.Fatalf("throwing proxy completion = %q, err=%v", got, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	got, err = s.Complete(ctx, "loopingCompletionProxy.a")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("looping proxy completion error = %v, want deadline exceeded", err)
	}
	if len(got) != 0 {
		t.Fatalf("looping proxy returned candidates after timeout: %q", got)
	}
	result := s.Eval(context.Background(), `42`)
	if result.Exit || result.Err != nil || result.Output != "42" {
		t.Fatalf("Eval after completion timeout = %+v", result)
	}
}

func TestSessionCompleteDiscardsProxyQueuedReaction(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	setup := s.Eval(context.Background(), `let queuedCompletionProxy = new Proxy({}, { ownKeys() { Promise.resolve().then(() => { globalThis.staleCompletionReaction = true; }); return ["candidate"]; } })`)
	if setup.Exit || setup.Err != nil {
		t.Fatalf("proxy setup = %+v", setup)
	}
	got, err := s.Complete(context.Background(), "queuedCompletionProxy.c")
	if err != nil || !containsString(got, "queuedCompletionProxy.candidate") {
		t.Fatalf("queued proxy completion = %q, err=%v", got, err)
	}
	result := s.Eval(context.Background(), `Object.hasOwn(globalThis, "staleCompletionReaction")`)
	if result.Exit || result.Err != nil || result.Output != "false" {
		t.Fatalf("queued completion reaction leaked into Eval: %+v", result)
	}
}

func TestSessionCompletePropagatesExplicitExit(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}

	setup := s.Eval(context.Background(), `let exitingCompletionProxy = new Proxy({}, { ownKeys() { noe.exit(7); return []; } })`)
	if setup.Exit || setup.Err != nil {
		t.Fatalf("proxy setup = %+v", setup)
	}
	_, err = s.Complete(context.Background(), "exitingCompletionProxy.a")
	var terminated *TerminationError
	if !errors.As(err, &terminated) || terminated.Code != 7 || terminated.Cause != nil {
		t.Fatalf("Complete after noe.exit = %#v, want exit code 7", err)
	}
	if err.Error() == "" {
		t.Fatal("TerminationError has an empty message")
	}
}

func TestSessionCompleteReportsClosedRuntime(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	_, err = s.Complete(context.Background(), "console")
	var terminated *TerminationError
	if !errors.As(err, &terminated) || terminated.Code != 1 || !errors.Is(err, quickjs.ErrClosed) {
		t.Fatalf("Complete on closed session = %#v, want code 1 wrapping ErrClosed", err)
	}
}

func TestSessionCompleteNameScannerHandlesNestedTemplatesAndStatementRegex(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	source := "const reviewMessage = `nested: ${`let reviewPhantomBinding = 1;`}`"
	result := s.Eval(context.Background(), source)
	if result.Exit || result.Err != nil {
		t.Fatalf("nested-template setup = %+v", result)
	}
	got, err := s.Complete(context.Background(), "reviewPhantom")
	if err != nil || containsString(got, "reviewPhantomBinding") {
		t.Fatalf("nested-template literal produced a declaration: candidates=%q err=%v", got, err)
	}

	result = s.Eval(context.Background(), `if (true) /{/; const reviewAfterRegex = 42`)
	if result.Exit || result.Err != nil {
		t.Fatalf("statement-regex setup = %+v", result)
	}
	got, err = s.Complete(context.Background(), "reviewAfterR")
	if err != nil || !containsString(got, "reviewAfterRegex") {
		t.Fatalf("statement regex hid following declaration: candidates=%q err=%v", got, err)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func isSortedUnique(values []string) bool {
	for i := 1; i < len(values); i++ {
		if strings.Compare(values[i-1], values[i]) >= 0 {
			return false
		}
	}
	return true
}
