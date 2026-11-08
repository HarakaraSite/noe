package stdlib_test

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestMultipartUploadTextAndFileBytes(t *testing.T) {
	wantFile := []byte("\x00\xfffile\nbytes\runchanged\r\n\x80")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var names []string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
			}
			names = append(names, part.FormName())
			switch part.FormName() {
			case "note":
				if string(data) != "one\r\ntwo\r\nthree\r\n東京" {
					t.Errorf("normalized text=%q", data)
				}
			case "upload":
				if part.FileName() != "sample.bin" || !bytes.Equal(data, wantFile) {
					t.Errorf("file name=%q bytes=%x", part.FileName(), data)
				}
			case "empty":
				if len(data) != 0 {
					t.Errorf("empty field=%q", data)
				}
			default:
				t.Errorf("unexpected field=%q", part.FormName())
			}
		}
		if fmt.Sprint(names) != "[note upload empty]" {
			t.Errorf("part order=%v", names)
		}
		io.WriteString(w, "standard MIME reader accepted upload")
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}}}, fmt.Sprintf(`(async()=>{
 const form=new FormData();form.append("note","one\ntwo\rthree\r\n東京");
 form.append("upload",new File([new Uint8Array([0,255]),"file\nbytes\runchanged\r\n",new Uint8Array([128])],"sample.bin"));form.append("empty","");
 const response=await fetch(%q,{method:"POST",body:form});
 if(!response.ok||await response.text()!=="standard MIME reader accepted upload")throw Error("multipart upload failed");
 if(form.get("note")!=="one\ntwo\rthree\r\n東京")throw Error("FormData changed");
 console.log("multipart upload passed");
})()`, server.URL))
	if out != "multipart upload passed" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestMultipartDownloadKeepsInlineMarkers(t *testing.T) {
	wantFile := []byte("\x00\xffbefore--fixtureafter\r\n\x80\r\n")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.SetBoundary("fixture"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("note", "before--fixtureafter\r\n"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("upload", "sample.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(wantFile); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", writer.FormDataContentType())
		w.Write(body.Bytes())
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 4 * time.Second}}}, fmt.Sprintf(`(async()=>{
 const response=await fetch(%q),form=await response.formData(),file=form.get("upload");
 if(form.get("note")!=="before--fixtureafter\r\n")throw Error("inline text marker truncated data");
 if(!(file instanceof File)||file.name!=="sample.bin"||Array.from(await file.bytes(),value=>value.toString(16).padStart(2,"0")).join("")!==%q)throw Error("inline file marker changed bytes");
 console.log("multipart download passed");
})()`, server.URL, fmt.Sprintf("%x", wantFile)))
	if out != "multipart download passed" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}
