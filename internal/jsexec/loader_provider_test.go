package jsexec

import (
	"context"
	"strings"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type testLoaderProvider struct {
	support URLImportSupport
	calls   int
	origin  Origin
}

func (p *testLoaderProvider) URLImports() URLImportSupport { return p.support }
func (p *testLoaderProvider) NewLoader(ctx context.Context, origin Origin) (quickjs.ModuleLoader, error) {
	p.calls++
	p.origin = origin
	return func(specifier, referrer string) (string, string, error) {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		return "export const value=42;", "test:module", nil
	}, nil
}

func TestURLImportCapabilityUsesOnlyProviderMetadata(t *testing.T) {
	legacy := LoaderFactory(func(context.Context, Origin) (quickjs.ModuleLoader, error) {
		t.Fatal("metadata lookup invoked legacy factory")
		return nil, nil
	})
	for _, tc := range []struct {
		name    string
		profile Profile
		want    URLImportSupport
	}{
		{"none", Profile{}, URLImportsDisabled},
		{"legacy", Profile{NewLoader: legacy}, URLImportsUnknown},
		{"enabled", Profile{Loader: &testLoaderProvider{support: URLImportsEnabled}}, URLImportsEnabled},
		{"disabled", Profile{Loader: &testLoaderProvider{support: URLImportsDisabled}}, URLImportsDisabled},
		{"unknown", Profile{Loader: &testLoaderProvider{support: URLImportsUnknown}}, URLImportsUnknown},
		{"unrecognized", Profile{Loader: &testLoaderProvider{support: "future"}}, URLImportsUnknown},
		{"conflict", Profile{Loader: &testLoaderProvider{support: URLImportsEnabled}, NewLoader: legacy}, URLImportsUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := URLImportCapability(tc.profile); got != tc.want {
				t.Fatalf("capability=%q want %q", got, tc.want)
			}
		})
	}
}

func TestProfileLoaderConflictRejectedBeforeFactory(t *testing.T) {
	provider := &testLoaderProvider{support: URLImportsEnabled}
	profile := Profile{Loader: provider, NewLoader: func(context.Context, Origin) (quickjs.ModuleLoader, error) {
		t.Fatal("conflicting factory was invoked")
		return nil, nil
	}}
	if _, err := Snapshot(profile); err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("Snapshot conflict error=%v", err)
	}
	if _, err := NewRuntime(t.Context(), profile); err == nil || !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("NewRuntime conflict error=%v", err)
	}
	if provider.calls != 0 {
		t.Fatal("conflicting provider was invoked")
	}
}

func TestProfileSnapshotBorrowsProviderAndSelectsFactory(t *testing.T) {
	provider := &testLoaderProvider{support: URLImportsEnabled}
	frozen, err := Snapshot(Profile{Loader: provider})
	if err != nil || frozen.Loader != provider || provider.calls != 0 {
		t.Fatalf("Snapshot=%+v err=%v calls=%d", frozen, err, provider.calls)
	}
	result, err := Execute(t.Context(), Source{Name: "[provider]", Virtual: true, Module: true, Code: `import {value} from "dependency";export default value;`}, frozen)
	if err != nil || string(result.Data) != "42" || provider.calls != 1 || provider.origin.Source == nil || !provider.origin.Virtual {
		t.Fatalf("result=%+v error=%v calls=%d origin=%+v", result, err, provider.calls, provider.origin)
	}
}
