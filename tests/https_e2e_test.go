//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPSAndWSSExampleThroughCLI(t *testing.T) {
	// The fixture supplies a test identity and a matching explicit trust pool.
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	identity := fixture.TLS.Certificates[0]
	roots := fixture.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	fixture.Close()
	key, err := x509.MarshalPKCS8PrivateKey(identity.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	certFile, keyFile := filepath.Join(work, "cert.pem"), filepath.Join(work, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: identity.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600); err != nil {
		t.Fatal(err)
	}
	p := startCLIWithOpenStdin(t, work, "run", filepath.Join(repoRootPath, "examples/http/https-server.js"), certFile, keyFile, "0")
	if !p.stdout.waitFor("\n", 3*time.Second) {
		t.Fatalf("HTTPS example did not start: %s", p.stderr.String())
	}
	address := strings.TrimSpace(p.stdout.String())
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Hostname() != "127.0.0.1" {
		t.Fatalf("server URL=%q error=%v", address, err)
	}
	clientTLS := &tls.Config{RootCAs: roots, NextProtos: []string{"h2", "http/1.1"}}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS.Clone(), ForceAttemptHTTP2: true}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ method, path, body, want string }{
		{"GET", "/health", "", "ok\n"},
		{"POST", "/echo", `{"message":"encrypted"}`, `{"message":"encrypted"}`},
	} {
		request, err := http.NewRequest(tc.method, address+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != 200 || string(body) != tc.want || response.TLS == nil || response.ProtoMajor != 1 {
			t.Fatalf("HTTPS response: status=%d body=%q proto=%s error=%v", response.StatusCode, body, response.Proto, readErr)
		}
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", u.Host, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if conn.ConnectionState().NegotiatedProtocol != "http/1.1" {
		t.Fatal("HTTPS listener negotiated an unsupported protocol")
	}
	if _, err := fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n", u.Host); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 || response.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("WSS upgrade: %s", response.Status)
	}
	for _, tc := range []struct {
		opcode byte
		data   []byte
	}{
		{2, []byte{0, 128, 255}},
		{1, []byte("encrypted text")},
		{8, []byte{3, 232, 'd', 'o', 'n', 'e'}},
	} {
		frame := []byte{0x80 | tc.opcode, 0x80 | byte(len(tc.data)), 1, 2, 3, 4}
		for i, b := range tc.data {
			frame = append(frame, b^byte(i%4+1))
		}
		if _, err := conn.Write(frame); err != nil {
			t.Fatal(err)
		}
		var head [2]byte
		if _, err := io.ReadFull(reader, head[:]); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, int(head[1]&0x7f))
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatal(err)
		}
		if head[0] != 0x80|tc.opcode || head[1]&0x80 != 0 || !bytes.Equal(body, tc.data) {
			t.Fatalf("WSS echo: opcode=%d masked=%v payload=%v", head[0]&0x0f, head[1]&0x80 != 0, body)
		}
	}
	conn.Close()
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	requireExit(t, p.wait(t, 3*time.Second), 1)
	requireServeRebind(t, address)
}
