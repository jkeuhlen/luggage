package app

import "testing"

func TestParseEpochSecondsToMS(t *testing.T) {
	ms, err := parseEpochSecondsToMS("1700000000.250")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ms != 1700000000250 {
		t.Fatalf("unexpected ms: %d", ms)
	}
}

func TestFirstToken(t *testing.T) {
	if got := firstToken("gs -sb"); got != "gs" {
		t.Fatalf("expected gs, got %q", got)
	}
}
