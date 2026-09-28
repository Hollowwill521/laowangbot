package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/OrionG-hub/laowangbot/internal/httpx"
	"github.com/OrionG-hub/laowangbot/internal/sourceplugin"
)

func releaseAsset(release *release, name string) (string, error) {
	found := ""
	for _, asset := range release.Assets {
		if asset.Name == name {
			if found != "" || asset.URL == "" {
				return "", fmt.Errorf("invalid or duplicate release asset: %s", name)
			}
			found = asset.URL
		}
	}
	if found == "" {
		return "", fmt.Errorf("release asset missing: %s", name)
	}
	return found, nil
}

func checksumFor(checksums []byte, name string) (string, error) {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if want != "" {
				return "", fmt.Errorf("ambiguous checksum: %s", name)
			}
			want = fields[0]
		}
	}
	raw, err := hex.DecodeString(want)
	if err != nil || len(raw) != sha256.Size {
		return "", fmt.Errorf("missing or invalid checksum: %s", name)
	}
	return strings.ToLower(want), nil
}

func replaceOfficialBinary(ctx context.Context, root, binary string, release *release) error {
	b := sourceplugin.Builder{Root: root, Binary: binary}
	return b.InstallBinary(ctx, release.TagName, func(candidate string) error {
		return downloadOfficialBinary(ctx, release, candidate)
	})
}

func downloadOfficialBinary(ctx context.Context, release *release, candidate string) error {
	name := fmt.Sprintf("laowangbot-%s-%s", runtime.GOOS, runtime.GOARCH)
	assetURL, err := releaseAsset(release, name)
	if err != nil {
		return err
	}
	checksumURL, err := releaseAsset(release, "checksums.txt")
	if err != nil {
		return err
	}
	checksums, err := httpx.Do(ctx, httpx.Request{URL: checksumURL, Timeout: 30 * time.Second, MaxBytes: 1 << 20})
	if err != nil {
		return err
	}
	if !checksums.OK() {
		return &httpx.StatusError{Status: checksums.Status}
	}
	want, err := checksumFor(checksums.Body, name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	digest := sha256.New()
	n, err := httpx.Download(ctx, httpx.Request{URL: assetURL, Timeout: 10 * time.Minute, MaxBytes: 100 << 20}, io.MultiWriter(f, digest))
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("downloaded binary is empty")
	}
	if hex.EncodeToString(digest.Sum(nil)) != want {
		return fmt.Errorf("checksum mismatch: %s", name)
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
