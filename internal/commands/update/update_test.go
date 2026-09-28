package update

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewerVersion(t *testing.T) {
	if !newer("", "v1.0.0") || !newer("dev", "v1.0.0") || !newer("0.1.0", "v0.2.0") {
		t.Error("a new release should be detected")
	}
	if newer("v1.2.3", "1.2.3") {
		t.Error("the same version with a different v prefix is not newer")
	}
}

func TestBuildFailureFitsTelegramLimit(t *testing.T) {
	text := buildFailure(errors.New(strings.Repeat("编译错误", 2000)))
	if utf8.RuneCountInString(text) > 2000 || !strings.Contains(text, "截断") {
		t.Fatal("unbounded build diagnostic")
	}
	if !strings.Contains(buildFailure(errors.New("missing Go")), "missing Go") {
		t.Fatal("lost error")
	}
}
