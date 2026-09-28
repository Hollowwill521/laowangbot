package media

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func tgsFixture(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, e := w.Write([]byte(s)); e != nil {
		t.Fatal(e)
	}
	w.Close()
	return b.Bytes()
}
func TestTGSValidation(t *testing.T) {
	for _, v := range []string{`{}`, `{"w":512,"h":512,"fr":0,"ip":0,"op":60}`, `{"w":9000,"h":512,"fr":30,"ip":0,"op":60}`, `{"w":512,"h":512,"fr":30,"ip":60,"op":60}`, `{"w":512,"h":512,"fr":30,"ip":0,"op":6000}`, `{"w":512,"h":512,"fr":30,"ip":0,"op":60,"assets":[{"p":"/etc/passwd"}]}`} {
		if _, e := parseTGS(tgsFixture(t, v)); e == nil {
			t.Errorf("accepted %s", v)
		}
	}
	if _, e := parseTGS(tgsFixture(t, strings.Repeat(" ", 2<<20))); e == nil {
		t.Fatal("accepted oversized decompression")
	}
	if _, e := parseTGS([]byte("not gzip")); e == nil {
		t.Fatal("accepted garbage")
	}
}
func TestTGSPlanPreservesDuration(t *testing.T) {
	p, e := parseTGS(tgsFixture(t, `{"w":512,"h":256,"fr":60,"ip":0,"op":180}`))
	if e != nil {
		t.Fatal(e)
	}
	if p.Frames != 90 || p.FPS != 30 || p.Width != 512 || p.Height != 256 {
		t.Fatalf("%+v", p)
	}
}
