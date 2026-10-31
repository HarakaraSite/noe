package credentials

import (
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodexImageCallbackPortCandidates(t *testing.T) {
	if got := codexCallbackPorts(Options{}); !slices.Equal(got, []int{1455, 1457}) {
		t.Fatalf("default registered callback ports = %v", got)
	}
	for _, port := range []int{1455, 1457, 49173} {
		if got := codexCallbackPorts(Options{CodexCallbackPort: port}); !slices.Equal(got, []int{port}) {
			t.Fatalf("explicit callback port was not preserved: %v", got)
		}
	}
}

func TestCodexImageCallbackChoosesAvailablePortWithoutCancelingOwner(t *testing.T) {
	var requests atomic.Int32
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer owner.Close()
	primaryPort := owner.Listener.Addr().(*net.TCPAddr).Port
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fallbackPort := reserved.Addr().(*net.TCPAddr).Port
	_ = reserved.Close()
	callback, err := listenCodexImageCallback([]int{primaryPort, fallbackPort})
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Close()
	if callback.Addr().(*net.TCPAddr).Port != fallbackPort {
		t.Fatal("occupied primary port did not select the available callback port")
	}
	if requests.Load() != 0 {
		t.Fatal("callback selection sent a request to the existing listener")
	}
	if listener, err := listenCodexImageCallback([]int{primaryPort, fallbackPort}); err == nil {
		listener.Close()
		t.Fatal("both occupied callback ports did not fail")
	} else if !strings.Contains(err.Error(), "finish a previous login") {
		t.Fatal("unavailable ports did not explain how to retry")
	}
	response, err := owner.Client().Get(owner.URL)
	if err != nil {
		t.Fatal("callback selection disturbed the existing listener")
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || requests.Load() != 1 {
		t.Fatal("existing listener did not remain usable")
	}
	if listener, err := listenCodexImageCallback(codexCallbackPorts(Options{CodexCallbackPort: primaryPort})); err == nil {
		listener.Close()
		t.Fatal("an occupied explicit callback port silently changed ports")
	}
	connection, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(fallbackPort)))
	if err != nil {
		t.Fatal("selected callback listener is no longer bound")
	}
	_ = connection.Close()
}
