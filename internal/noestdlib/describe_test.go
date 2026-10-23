package noestdlib

import (
	"errors"
	quickjs "github.com/go-quickjs/go-quickjs"
	"testing"
)

func TestDescribeDistinguishesExportsAndReference(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	if err := installDocs(rt, t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := rt.DefineSyntheticModule("custom", []string{"undocumented", "then"}, func() (map[string]any, error) { calls++; return map[string]any{"undocumented": 42, "then": nil}, nil }); err != nil {
		t.Fatal(err)
	}
	// Synthetic modules need a loader-resolved name; registration alone does
	// not make their specifier importable as SetModule does.
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		if specifier == "custom" {
			return "", "custom", nil
		}
		return "", "", errors.New("unregistered module")
	})
	value, err := describe(t.Context(), rt, "custom", "undocumented")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || value["availability"] != "available" || value["documented"] != false {
		t.Fatalf("unexpected description: %#v, body calls %d", value, calls)
	}
	member := value["member"].(map[string]any)
	if member["availability"] != "available" || member["documented"] != false || member["signature"] != nil {
		t.Fatalf("undocumented export lost: %#v", member)
	}
	unknown, err := describe(t.Context(), rt, "unregistered-custom", "")
	if err != nil || unknown["availability"] != "unknown" {
		t.Fatalf("unknown custom incorrectly classified: %#v %v", unknown, err)
	}
}

func TestDescribeSourceDoesNotEvaluateBody(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		return `throw Error("must not run"); export const value=42;`, specifier, nil
	})
	value, err := describe(t.Context(), rt, "custom-source", "value")
	if err != nil || value["availability"] != "available" {
		t.Fatalf("non-evaluating source inspection: %#v %v", value, err)
	}
}

func TestDescribeSignatureUsesAuthoredReference(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	if err := installDocs(rt, t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := describe(t.Context(), rt, "noe:docs", "describe")
	if err != nil {
		t.Fatal(err)
	}
	member := value["member"].(map[string]any)
	if member["documented"] != true || member["signature"] == nil || member["availability"] != "available" {
		t.Fatalf("description metadata: %#v", member)
	}
}
