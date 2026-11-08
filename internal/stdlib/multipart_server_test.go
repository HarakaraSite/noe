package stdlib

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"
)

func TestServeMultipartInlineMarkers(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{
 const form=await request.formData();
 if(form.get("note")!=="before--fixtureafter\nraw")throw Error("incoming text changed");
 const bytes=await form.get("upload").bytes();
 if(bytes.join()!=="0,255,88,45,45,102,105,120,116,117,114,101,89,13,10")throw Error("incoming file changed");
 return new Response("incoming multipart passed");
 }`)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.SetBoundary("fixture"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("note", "before--fixtureafter\nraw"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("upload", "sample.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("\x00\xffX--fixtureY\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Post(rig.url, writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(data) != "incoming multipart passed" {
		t.Fatalf("status=%d body=%q err=%v", response.StatusCode, data, err)
	}
}
