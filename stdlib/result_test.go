package stdlib_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestPublicResultCarriesRawDataAndStructuredDiagnostics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	dataResult, err := stdlib.Execute(ctx, stdlib.Source{Name: "[public result]", Module: true, Virtual: true, Code: `export default {answer:42}`}, stdlib.Profile{})
	if err != nil || string(dataResult.Data) != `{"answer":42}` || len(dataResult.Diagnostics) != 0 {
		t.Fatalf("data result=%#v error=%v", dataResult, err)
	}

	diagnosticResult, err := stdlib.Execute(ctx, stdlib.Source{Name: "[public diagnostic]", Module: true, Virtual: true, Code: `export default undefined`}, stdlib.Profile{})
	if err == nil || diagnosticResult.Code != 1 || diagnosticResult.Data != nil || len(diagnosticResult.Diagnostics) != 1 {
		t.Fatalf("diagnostic result=%#v error=%v", diagnosticResult, err)
	}
	diagnostic := diagnosticResult.Diagnostics[0]
	if diagnostic.Code != "serialization_error" || diagnostic.Stage != "serialize" || !strings.Contains(stdlib.Diagnostic(err), "could not be serialized") {
		t.Fatalf("diagnostic=%#v legacy=%q", diagnostic, stdlib.Diagnostic(err))
	}

	// These aliases keep shared details usable without exposing engine types.
	var _ stdlib.ExecutionDiagnostic = diagnostic
	var _ stdlib.DiagnosticPosition
	var _ stdlib.DiagnosticEncodedBytes

	encoded, marshalErr := json.Marshal(diagnosticResult)
	if marshalErr != nil || !strings.Contains(string(encoded), `"diagnostics":[{"code":"serialization_error","stage":"serialize"`) {
		t.Fatalf("diagnostic json=%s error=%v", encoded, marshalErr)
	}
}
