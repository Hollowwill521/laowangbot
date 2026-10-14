package config

import "testing"

func TestEnvCanonicalNamePrecedesLegacy(t *testing.T) {
	env := Env{"MIBOT_SERVICE": "legacy.service", "LAOWANGBOT_SERVICE": "new.service"}
	if got := env.Get("LAOWANGBOT_SERVICE", "fallback"); got != "new.service" {
		t.Fatalf("canonical value = %q", got)
	}
	if got := env.Get("MIBOT_SERVICE", "fallback"); got != "new.service" {
		t.Fatalf("legacy lookup did not prefer canonical value: %q", got)
	}
}

func TestEnvCanonicalEmptyMasksLegacy(t *testing.T) {
	env := Env{"MIBOT_PREFIX": ".", "LAOWANGBOT_PREFIX": ""}
	if got := env.Get("LAOWANGBOT_PREFIX", "fallback"); got != "" {
		t.Fatalf("explicit canonical empty value = %q", got)
	}
	if got := env.Prefixes(); len(got) != 4 {
		t.Fatalf("empty canonical prefix should use defaults, got %v", got)
	}
}

func TestEnvLegacyFallback(t *testing.T) {
	env := Env{"MIBOT_SERVICE": "legacy.service"}
	if got := env.Get("LAOWANGBOT_SERVICE", "fallback"); got != "legacy.service" {
		t.Fatalf("legacy fallback = %q", got)
	}
}
