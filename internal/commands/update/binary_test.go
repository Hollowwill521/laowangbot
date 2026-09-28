package update

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChecksumManifest(t *testing.T) {
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("binary")))
	for _, marker := range []string{"", "*"} {
		got, err := checksumFor([]byte(sum+"  "+marker+"bot\n"), "bot")
		if err != nil || got != sum {
			t.Fatal(got, err)
		}
	}
	for _, bad := range []string{"", sum + " other", strings.Repeat("z", 64) + " bot", sum + " bot\n" + sum + " bot"} {
		if _, err := checksumFor([]byte(bad), "bot"); err == nil {
			t.Fatal("accepted invalid checksum manifest")
		}
	}
}

func TestOfficialDownloadValidation(t *testing.T) {
	name := fmt.Sprintf("laowangbot-%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, mode := range []string{"valid", "hash", "missing", "duplicate", "http", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("binary fixture")
			sum := fmt.Sprintf("%x", sha256.Sum256(data))
			var base string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/checksums" {
					if mode == "hash" {
						sum = strings.Repeat("0", 64)
					}
					fmt.Fprintf(w, "%s  %s\n", sum, name)
					return
				}
				if mode == "http" {
					w.WriteHeader(503)
					return
				}
				if mode == "truncated" {
					w.Header().Set("Content-Length", "100")
				}
				w.Write(data)
			}))
			defer server.Close()
			base = server.URL
			var release release
			raw := fmt.Sprintf(`{"tag_name":"v2.0.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`, name, base+"/binary", base+"/checksums")
			if err := json.Unmarshal([]byte(raw), &release); err != nil {
				t.Fatal(err)
			}
			if mode == "missing" {
				release.Assets = release.Assets[:1]
			}
			if mode == "duplicate" {
				release.Assets = append(release.Assets, release.Assets[0])
			}
			dest := filepath.Join(t.TempDir(), "candidate")
			err := downloadOfficialBinary(t.Context(), &release, dest)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := os.ReadFile(dest)
				if string(got) != string(data) {
					t.Fatal("corrupted binary")
				}
			} else if err == nil {
				t.Fatalf("accepted %s", mode)
			}
		})
	}
}
