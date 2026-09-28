//go:build integration

package yvlu

import (
	"encoding/base64"
	"github.com/OrionG-hub/laowangbot/internal/media"
	"os"
	"strings"
	"testing"
)

func TestQuotedTGSBecomesAnimatedMedia(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_TGS") != "1" {
		t.Skip("enable TGS renderer integration with LAOWANGBOT_TEST_TGS=1")
	}
	input, e := os.ReadFile("../../media/testdata/moving.tgs")
	if e != nil {
		t.Fatal(e)
	}
	rpc := &stickerRPC{data: input}
	inv, msg := stickerInvocation(rpc, "application/x-tgsticker", int64(len(input)))
	item := quoteMessage{}
	if e = (&yvluService{}).describeMedia(t.Context(), inv, msg, &item); e != nil {
		t.Fatal(e)
	}
	if item.Media == nil || !strings.HasPrefix(item.Media.URL, "data:video/webm;base64,") {
		t.Fatalf("TGS missing from payload: %+v", item)
	}
	movie, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(item.Media.URL, "data:video/webm;base64,"))
	if e != nil {
		t.Fatal(e)
	}
	w, h, duration, e := media.WebMInfo(movie)
	if e != nil || w != 64 || h != 64 || duration < 0.9 {
		t.Fatal(w, h, duration, e)
	}
}
