package stdlib

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type serveBodyLimitProbe struct {
	reader *bytes.Reader
	sizes  []int
}

func (p *serveBodyLimitProbe) Read(b []byte) (int, error) {
	p.sizes = append(p.sizes, len(b))
	return p.reader.Read(b)
}

func (*serveBodyLimitProbe) Close() error { return nil }

func TestServeBodyLimitBoundaryProbe(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		probe := &serveBodyLimitProbe{reader: bytes.NewReader([]byte("abc"))}
		body := &serveBody{body: probe, remaining: 3}

		data, err := body.read()
		if err != nil || string(data) != "abc" {
			t.Fatalf("first read = %q, %v", data, err)
		}
		data, err = body.read()
		if len(data) != 0 || err != io.EOF || !body.complete() {
			t.Fatalf("boundary probe = %q, %v, complete=%t", data, err, body.complete())
		}
		if len(probe.sizes) != 2 || probe.sizes[0] != 3 || probe.sizes[1] != 1 {
			t.Fatalf("read sizes = %v, want [3 1]", probe.sizes)
		}
	})

	t.Run("overflow", func(t *testing.T) {
		probe := &serveBodyLimitProbe{reader: bytes.NewReader([]byte("abcd"))}
		body := &serveBody{body: probe, remaining: 3}

		data, err := body.read()
		if err != nil || string(data) != "abc" {
			t.Fatalf("permitted read = %q, %v", data, err)
		}
		data, err = body.read()
		if len(data) != 0 || !errors.Is(err, errServeBodyTooLarge) {
			t.Fatalf("overflow read = %q, %v", data, err)
		}
		if _, err := body.read(); !errors.Is(err, errServeBodyTooLarge) {
			t.Fatalf("repeated overflow read = %v", err)
		}
		if probe.reader.Len() != 0 || len(probe.sizes) != 2 || probe.sizes[0] != 3 || probe.sizes[1] != 1 {
			t.Fatalf("probe consumed more than one byte: remaining=%d sizes=%v", probe.reader.Len(), probe.sizes)
		}
	})

	t.Run("zero limit", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			data string
			want error
		}{{"empty", "", io.EOF}, {"nonempty", "x", errServeBodyTooLarge}} {
			t.Run(tc.name, func(t *testing.T) {
				probe := &serveBodyLimitProbe{reader: bytes.NewReader([]byte(tc.data))}
				body := &serveBody{body: probe, remaining: 0}
				data, err := body.read()
				if len(data) != 0 || !errors.Is(err, tc.want) {
					t.Fatalf("read = %q, %v; want %v", data, err, tc.want)
				}
				if len(probe.sizes) != 1 || probe.sizes[0] != 1 {
					t.Fatalf("read sizes = %v, want [1]", probe.sizes)
				}
			})
		}
	})

	t.Run("negative is unlimited and remains chunked", func(t *testing.T) {
		input := bytes.Repeat([]byte{42}, serveReadSize+7)
		probe := &serveBodyLimitProbe{reader: bytes.NewReader(input)}
		body := &serveBody{body: probe, remaining: -1}
		var got []byte
		for {
			data, err := body.read()
			got = append(got, data...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(got, input) {
			t.Fatalf("read %d bytes, want %d", len(got), len(input))
		}
		for _, size := range probe.sizes {
			if size != serveReadSize {
				t.Fatalf("native read size = %d, want %d", size, serveReadSize)
			}
		}
	})
}

func TestServeBodyLimitCollectorAndStreamErrors(t *testing.T) {
	cases := []struct {
		name, handler, want string
	}{
		{
			name:    "collector",
			handler: `async request=>{try{return new Response(await request.text())}catch(error){return new Response(error.name+"|"+error.message+"|"+error.code)}}`,
			want:    "RangeError|request body exceeds configured limit|ERR_HTTP_BODY_TOO_LARGE",
		},
		{
			name:    "streamed reader gets prefix then error",
			handler: `async request=>{const reader=request.body.getReader();const first=await reader.read();try{await reader.read();return new Response("unexpected")}catch(error){return new Response(new TextDecoder().decode(first.value)+"|"+error.name+"|"+error.message+"|"+error.code)}}`,
			want:    "ab|RangeError|request body exceeds configured limit|ERR_HTTP_BODY_TOO_LARGE",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newReceiveRig(t, 2, tc.handler)
			client := &http.Client{Timeout: 4 * time.Second}
			defer client.CloseIdleConnections()
			res, err := client.Post(rig.url, "text/plain", strings.NewReader("abcd"))
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if res.StatusCode != http.StatusOK || string(data) != tc.want {
				t.Fatalf("response = %d %q, want 200 %q", res.StatusCode, data, tc.want)
			}
		})
	}
}

func TestServeBodyLimitExactRequestAndBoundaryCancellation(t *testing.T) {
	t.Run("exact request body succeeds", func(t *testing.T) {
		rig := newReceiveRig(t, 3, `async request=>new Response(await request.text())`)
		client := &http.Client{Timeout: 4 * time.Second}
		defer client.CloseIdleConnections()
		res, err := client.Post(rig.url, "text/plain", strings.NewReader("abc"))
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if res.StatusCode != http.StatusOK || string(data) != "abc" {
			t.Fatalf("response = %d %q", res.StatusCode, data)
		}
	})

	t.Run("cancel blocked boundary probe before final response", func(t *testing.T) {
		rig := newReceiveRig(t, 2, `async request=>{const reader=request.body.getReader();const first=await reader.read();if(new TextDecoder().decode(first.value)!=="ab")throw Error("bad prefix");const pending=reader.read();notify("boundary");await reader.cancel();await pending;return new Response("cancelled")}`)
		conn, reader := receiveConn(t, rig, "Content-Length: 20\r\nExpect: 100-continue\r\n")
		if res := receiveResponse(t, reader); res.StatusCode != http.StatusContinue {
			t.Fatalf("interim status = %d", res.StatusCode)
		}
		if _, err := io.WriteString(conn, "ab"); err != nil {
			t.Fatal(err)
		}
		rig.notice(t, "boundary")
		res := receiveResponse(t, reader)
		if res.StatusCode != http.StatusOK || receiveText(t, res) != "cancelled" {
			t.Fatalf("response = %d", res.StatusCode)
		}
	})
}

func TestServeBodyLimitDataAndEOFRemainVisible(t *testing.T) {
	terminal := errors.New("truncated request body")
	probe := &serveBodyErrorProbe{data: []byte("abc"), terminal: terminal}
	body := &serveBody{body: probe, remaining: 4}
	data, err := body.read()
	if err != nil || string(data) != "abc" {
		t.Fatalf("data read = %q, %v", data, err)
	}
	if data, err = body.read(); len(data) != 0 || !errors.Is(err, terminal) {
		t.Fatalf("terminal read = %q, %v", data, err)
	}
	if probe.reads != 1 {
		t.Fatalf("native reads = %d, want 1", probe.reads)
	}
}

type serveBodyErrorProbe struct {
	data     []byte
	terminal error
	reads    int
}

func (p *serveBodyErrorProbe) Read(b []byte) (int, error) {
	p.reads++
	n := copy(b, p.data)
	p.data = p.data[n:]
	if len(p.data) == 0 {
		return n, p.terminal
	}
	return n, nil
}

func (*serveBodyErrorProbe) Close() error { return nil }
