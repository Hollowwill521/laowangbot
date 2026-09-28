//go:build integration

package media

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTGSRealAnimation(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_TGS") != "1" {
		t.Skip("set LAOWANGBOT_TEST_TGS=1 with Python renderer and ffmpeg to run")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	input, e := os.ReadFile("testdata/moving.tgs")
	if e != nil {
		t.Fatal(e)
	}
	data, e := TGSWebM(ctx, dir, input)
	if e != nil {
		t.Fatal(e)
	}
	w, h, duration, e := WebMInfo(data)
	if e != nil || w != 64 || h != 64 || duration < 0.9 || duration > 1.1 {
		t.Fatalf("webm %d %d %f %v", w, h, duration, e)
	}
	binary, e := FFmpeg()
	if e != nil {
		t.Fatal(e)
	}
	movie := filepath.Join(dir, "animated.webm")
	// Decode actual encoded VP9 alpha frames, proving nonempty animated pixels survive.
	decode := exec.CommandContext(ctx, binary, "-v", "error", "-c:v", "libvpx-vp9", "-i", movie, "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	pixels, e := decode.Output()
	if e != nil {
		t.Fatal(e)
	}
	frame := 64 * 64 * 4
	if len(pixels) < frame*20 {
		t.Fatalf("too few decoded frames: %d", len(pixels))
	}
	a, b := pixels[:frame], pixels[19*frame:20*frame]
	if bytes.Equal(a, b) {
		t.Fatal("animation became static")
	}
	visible, transparent := 0, 0
	for i := 3; i < len(a); i += 4 {
		if a[i] > 0 {
			visible++
		} else {
			transparent++
		}
	}
	if visible == 0 || transparent == 0 {
		t.Fatalf("blank or lost alpha: visible=%d transparent=%d", visible, transparent)
	}
}

func TestTGSPartialAlphaPreservesColor(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_TGS") != "1" {
		t.Skip("requires TGS renderer")
	}
	input, e := os.ReadFile("testdata/moving.tgs")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := parseTGS(input)
	if e != nil {
		t.Fatal(e)
	}
	translucent := bytes.Replace(plan.JSON, []byte(`"o":{"a":0,"k":100}`), []byte(`"o":{"a":0,"k":50}`), 1)
	dir := t.TempDir()
	if _, e = TGSWebM(t.Context(), dir, tgsFixture(t, string(translucent))); e != nil {
		t.Fatal(e)
	}
	binary, e := FFmpeg()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(binary, "-v", "error", "-c:v", "libvpx-vp9", "-i", filepath.Join(dir, "animated.webm"), "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	pixels, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	i := (32*64 + 16) * 4
	if len(pixels) <= i+3 {
		t.Fatal("missing pixels")
	}
	if pixels[i] < 230 || pixels[i+3] < 100 || pixels[i+3] > 150 {
		t.Fatalf("partially transparent red darkened: %v", pixels[i:i+4])
	}
}

func TestTGSCanceledConversionProducesNoMovie(t *testing.T) {
	if os.Getenv("LAOWANGBOT_TEST_TGS") != "1" {
		t.Skip("requires TGS renderer")
	}
	input, e := os.ReadFile("testdata/moving.tgs")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	if _, e = TGSWebM(ctx, dir, input); e == nil {
		t.Fatal("canceled conversion succeeded")
	}
	if _, e = os.Stat(filepath.Join(dir, "animated.webm")); !os.IsNotExist(e) {
		t.Fatal("canceled conversion wrote output", e)
	}
}
