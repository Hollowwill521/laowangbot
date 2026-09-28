package media

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// TGS conversion is an on-demand helper process; the Go host has no embedded
// Python/JavaScript runtime. Render lossless RGBA PNG frames to retain alpha.
const renderTGS = `import json, sys
try:
    from rlottie_python import LottieAnimation
    from PIL import Image
except ImportError:
    raise SystemExit("TGS requires rlottie-python and Pillow in LAOWANGBOT_PYTHON")
with open(sys.argv[1], encoding="utf-8") as f:
    data = f.read()
source = json.loads(data)
anim = LottieAnimation.from_data(data)
frames, fps = int(sys.argv[2]), float(sys.argv[3])
width, height = int(sys.argv[4]), int(sys.argv[5])
for i in range(frames):
    frame = min(anim.lottie_animation_get_totalframe() - 1, int(i * source["fr"] / fps))
    im = anim.render_pillow_frame(frame_num=frame, width=width, height=height)
    # rlottie pixels are premultiplied; PNG and ffmpeg expect straight alpha.
    Image.frombytes("RGBa", im.size, im.tobytes()).convert("RGBA").save("frame-%03d.png" % i)
`

type tgsPlan struct {
	JSON                  []byte
	Width, Height, Frames int
	FPS                   float64
}

func parseTGS(input []byte) (tgsPlan, error) {
	var p tgsPlan
	if len(input) > 1<<20 {
		return p, errors.New("TGS compressed data exceeds 1 MiB")
	}
	z, e := gzip.NewReader(bytes.NewReader(input))
	if e != nil {
		return p, e
	}
	defer z.Close()
	raw, e := io.ReadAll(io.LimitReader(z, (1<<20)+1))
	if e != nil {
		return p, e
	}
	if len(raw) > 1<<20 {
		return p, errors.New("TGS JSON exceeds 1 MiB")
	}
	var v struct {
		Width  int     `json:"w"`
		Height int     `json:"h"`
		FPS    float64 `json:"fr"`
		Start  float64 `json:"ip"`
		End    float64 `json:"op"`
		Assets []struct {
			Path string `json:"p"`
		} `json:"assets"`
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return p, e
	}
	duration := (v.End - v.Start) / v.FPS
	if v.Width < 1 || v.Height < 1 || v.Width > 512 || v.Height > 512 || v.FPS <= 0 || v.FPS > 120 || v.Start < 0 || duration <= 0 || duration > 3 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return p, errors.New("invalid TGS dimensions, frame rate or duration (max 512px / 3s)")
	}
	for _, a := range v.Assets {
		if a.Path != "" {
			return p, errors.New("external TGS image assets are not supported")
		}
	}
	fps := math.Min(v.FPS, 30)
	return tgsPlan{JSON: raw, Width: v.Width, Height: v.Height, Frames: int(math.Ceil(duration * fps)), FPS: fps}, nil
}
func TGSWebM(ctx context.Context, directory string, input []byte) ([]byte, error) {
	plan, e := parseTGS(input)
	if e != nil {
		return nil, fmt.Errorf("TGS: %w", e)
	}
	ffmpeg, e := FFmpeg()
	if e != nil {
		return nil, e
	}
	python := os.Getenv("LAOWANGBOT_PYTHON")
	if python == "" {
		python = "python3"
	}
	python, e = exec.LookPath(python)
	if e != nil {
		return nil, errors.New("TGS requires Python 3 with rlottie-python and Pillow; set LAOWANGBOT_PYTHON")
	}
	source := filepath.Join(directory, "animation.json")
	if e = os.WriteFile(source, plan.JSON, 0600); e != nil {
		return nil, e
	}
	if e = run(ctx, python, directory, []string{"-c", renderTGS, source, strconv.Itoa(plan.Frames), strconv.FormatFloat(plan.FPS, 'f', 6, 64), strconv.Itoa(plan.Width), strconv.Itoa(plan.Height)}); e != nil {
		return nil, fmt.Errorf("TGS renderer (install rlottie-python Pillow): %w", e)
	}
	args := []string{"-nostdin", "-v", "error", "-framerate", strconv.FormatFloat(plan.FPS, 'f', 6, 64), "-i", "frame-%03d.png", "-frames:v", strconv.Itoa(plan.Frames), "-c:v", "libvpx-vp9", "-pix_fmt", "yuva420p", "-b:v", "400k", "-deadline", "good", "-cpu-used", "5", "-row-mt", "1", "-threads", "2", "-auto-alt-ref", "0", "-an", "-y", "animated.webm"}
	if e = run(ctx, ffmpeg, directory, args); e != nil {
		return nil, e
	}
	return readBounded(filepath.Join(directory, "animated.webm"), 8<<20)
}
