package save

import (
	"testing"

	"github.com/OrionG-hub/laowangbot/internal/bot"
)

func TestPartialPatternKeepsPhotoExtension(t *testing.T) {
	if got := partialPattern(&bot.MediaSource{Photo: true, MimeType: "image/jpeg"}); got != "part-*.jpg" {
		t.Fatalf("partialPattern(photo) = %q", got)
	}
}
