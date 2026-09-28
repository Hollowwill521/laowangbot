package pluginapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostCallAuthenticated(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.URL.Path != "/call" {
			t.Error("bad request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message_id":8}`))
	}))
	defer s.Close()
	h := Client{URL: s.URL, Token: "token", HTTP: s.Client()}
	r, e := h.Call(context.Background(), Call{Method: "send", Target: "me", Text: "test"})
	if e != nil || r.MessageID != 8 {
		t.Fatal(r, e)
	}
}
func TestProtocolLoop(t *testing.T) {
	var out strings.Builder
	e := ServeIO(context.Background(), strings.NewReader("{\"version\":1,\"type\":\"tick\"}\n"), &out, func(_ context.Context, q Request) Response { return Response{Text: q.Type} })
	if e != nil || !strings.Contains(out.String(), `"text":"tick"`) {
		t.Fatal(out.String(), e)
	}
}
func TestStateRoundTrip(t *testing.T) {
	path := t.TempDir() + "/state.json"
	if e := SaveJSON(path, map[string]string{"value": "test"}); e != nil {
		t.Fatal(e)
	}
	var result map[string]string
	if e := LoadJSON(path, &result); e != nil || result["value"] != "test" {
		t.Fatal(result, e)
	}
}
