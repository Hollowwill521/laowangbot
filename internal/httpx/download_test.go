package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type measuredWriter struct {
	count   int
	largest int
}

func (w *measuredWriter) Write(p []byte) (int, error) {
	w.count += len(p)
	if len(p) > w.largest {
		w.largest = len(p)
	}
	return len(p), nil
}
func TestDownloadStreamsAndBoundsUnknownLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		chunk := make([]byte, 4096)
		for range 512 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	var dst measuredWriter
	n, err := Download(t.Context(), Request{URL: server.URL, MaxBytes: 3 << 20}, &dst)
	if err != nil || n != 2<<20 || dst.largest > 32<<10 {
		t.Fatalf("n=%d largest=%d err=%v", n, dst.largest, err)
	}
	if _, err := Download(t.Context(), Request{URL: server.URL, MaxBytes: 1024}, io.Discard); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Download(ctx, Request{URL: server.URL}, io.Discard); err == nil {
		t.Fatal("ignored cancellation")
	}
}
