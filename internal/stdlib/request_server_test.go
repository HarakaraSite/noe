package stdlib

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeIncomingRequestKeepsNativeMetadata(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{
  const cloned=request.clone(),copied=new Request(cloned);
  if(request.method!==cloned.method||request.method!==copied.method)throw Error("incoming method changed");
  if(request.url!==cloned.url||request.url!==copied.url)throw Error("incoming URL changed");
  if(!request.url.endsWith("/a/../raw?x=%2f"))throw Error("incoming raw URL changed");
  if(await request.text()!=="payload"||await copied.text()!=="payload")throw Error("incoming clone/transfer changed");
  return new Response(request.method);
 }`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	for _, method := range []string{"TRACE", "TRACK", "patch", "pOsT"} {
		request, err := http.NewRequest(method, rig.url+"/a/../raw?x=%2f", strings.NewReader("payload"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || string(body) != method {
			t.Fatalf("%s: status=%d body=%q err=%v", method, response.StatusCode, body, err)
		}
	}
}
