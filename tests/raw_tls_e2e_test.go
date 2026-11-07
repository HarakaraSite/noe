//go:build e2e

package e2e_test

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRawTLSRoundTripExampleThroughCLI(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	identity := fixture.TLS.Certificates[0]
	fixture.Close()
	key, err := x509.MarshalPKCS8PrivateKey(identity.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	certFile, keyFile := filepath.Join(work, "cert.pem"), filepath.Join(work, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: identity.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	got := runCLI(t, work, nil, "run", filepath.Join(repoRootPath, "examples/low-level/tls-roundtrip.js"), certFile, keyFile)
	requireExit(t, got, 0)
	if got.stderr != "" || strings.TrimSpace(got.stdout) != "TLS roundtrip passed" {
		t.Fatalf("TLS example: %+v", got)
	}
}
