package version

import (
	"os"
	"strings"
	"testing"
)

func TestVersionFileMatchesSource(t *testing.T) {
	b, e := os.ReadFile("../../VERSION")
	if e != nil {
		t.Fatal(e)
	}
	if strings.TrimSpace(string(b)) != Current {
		t.Fatal("VERSION and source version must change together")
	}
}
