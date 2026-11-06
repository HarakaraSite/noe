package stdlib_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type limitedFetchTransport struct {
	data          []byte
	chunkSize     int
	contentLength int64
	mu            sync.Mutex
	body          *limitedFetchBody
}

func (t *limitedFetchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := &limitedFetchBody{data: t.data, chunkSize: t.chunkSize}
	t.mu.Lock()
	t.body = body
	t.mu.Unlock()
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Proto:         "HTTP/1.1",
		Header:        make(http.Header),
		Body:          body,
		ContentLength: t.contentLength,
		Request:       req,
	}, nil
}

func (t *limitedFetchTransport) responseBody() *limitedFetchBody {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.body
}

type limitedFetchBody struct {
	data      []byte
	chunkSize int
	offset    int
	readBytes atomic.Int64
	closed    atomic.Bool
}

func (b *limitedFetchBody) Read(p []byte) (int, error) {
	if b.offset == len(b.data) {
		return 0, io.EOF
	}
	n := len(p)
	if remaining := len(b.data) - b.offset; n > remaining {
		n = remaining
	}
	if b.chunkSize > 0 && n > b.chunkSize {
		n = b.chunkSize
	}
	copy(p[:n], b.data[b.offset:b.offset+n])
	b.offset += n
	b.readBytes.Add(int64(n))
	return n, nil
}

func (b *limitedFetchBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestFetchBodyLimitStreaming(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		contentLength int64
		source        string
		want          string
		wantRead      int64
	}{
		{
			name:          "exact limit convenience method",
			data:          "abcde",
			contentLength: 6,
			source: `fetch("http://fixture/").then(r => r.text()).then(
  text => console.log("ok", text), error => console.log("err", error.message, error.cause?.message)
)`,
			want:     "ok abcde",
			wantRead: 5,
		},
		{
			name:          "exact limit streamed chunks",
			data:          "abcde",
			contentLength: 4,
			source: `;(async () => {
  const reader = (await fetch("http://fixture/")).body.getReader();
  const chunks = [];
  for (;;) {
    const part = await reader.read();
    if (part.done) break;
    chunks.push(new TextDecoder().decode(part.value));
  }
  console.log(chunks.join(""), chunks.length);
})()`,
			want:     "abcde 3",
			wantRead: 5,
		},
		{
			name:          "over limit convenience method",
			data:          "abcdef",
			contentLength: 5,
			source: `fetch("http://fixture/").then(async r => {
  try { await r.bytes(); console.log("accepted"); }
  catch (error) { console.log(error instanceof TypeError, error.message, error.cause?.message); }
})`,
			want:     "true terminated response body exceeds configured limit",
			wantRead: 6,
		},
		{
			name:          "over limit streamed chunks",
			data:          "abcdef",
			contentLength: 6,
			source: `;(async () => {
  const reader = (await fetch("http://fixture/")).body.getReader();
  const chunks = [];
  let failure;
  try {
    for (;;) {
      const part = await reader.read();
      if (part.done) break;
      chunks.push(new TextDecoder().decode(part.value));
    }
  } catch (error) { failure = error; }
  console.log(chunks.join(""), chunks.length, failure instanceof TypeError, failure.message, failure.cause?.message);
})()`,
			want:     "abcde 3 true terminated response body exceeds configured limit",
			wantRead: 6,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &limitedFetchTransport{
				data:          []byte(test.data),
				chunkSize:     2,
				contentLength: test.contentLength,
			}
			out, stderr := run(t, stdlib.Config{
				Fetch: &stdlib.Fetch{
					Client:       &http.Client{Transport: transport},
					MaxBodyBytes: 5,
				},
			}, test.source)
			if out != test.want || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q, want stdout=%q and no stderr", out, stderr, test.want)
			}
			body := transport.responseBody()
			if body == nil || !body.closed.Load() {
				t.Fatal("response body was not closed")
			}
			if got := body.readBytes.Load(); got != test.wantRead {
				t.Fatalf("read %d response bytes, want %d", got, test.wantRead)
			}
		})
	}
}

func TestFetchBodyLimitBuffered(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		contentLength int64
		want          string
		wantRead      int64
	}{
		{name: "exact limit", data: "abcde", contentLength: 6, want: "ok abcde", wantRead: 5},
		{name: "one byte over", data: "abcdef", contentLength: 5, want: "err fetch failed response body exceeds configured limit", wantRead: 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &limitedFetchTransport{data: []byte(test.data), contentLength: test.contentLength}
			out, stderr := runBufferedFetch(t, transport, `fetch("http://fixture/").then(r => r.text()).then(
  text => console.log("ok", text), error => console.log("err", error.message, error.cause?.message)
)`)
			if out != test.want || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q, want stdout=%q and no stderr", out, stderr, test.want)
			}
			body := transport.responseBody()
			if body == nil || !body.closed.Load() {
				t.Fatal("response body was not closed")
			}
			if got := body.readBytes.Load(); got != test.wantRead {
				t.Fatalf("read %d response bytes, want %d", got, test.wantRead)
			}
		})
	}
}

func runBufferedFetch(t *testing.T, transport http.RoundTripper, source string) (string, string) {
	t.Helper()
	rt := quickjs.New()
	defer rt.Close()
	var out, stderr bytes.Buffer
	if err := stdlib.Install(rt, stdlib.Config{
		Fetch:  &stdlib.Fetch{Client: &http.Client{Transport: transport}, MaxBodyBytes: 5},
		Stdout: &out,
		Stderr: &stderr,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(source); err != nil {
		t.Fatal(err)
	}
	if err := rt.RunJobs(); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(out.String(), "\n"), strings.TrimRight(stderr.String(), "\n")
}

var _ io.ReadCloser = (*limitedFetchBody)(nil)
