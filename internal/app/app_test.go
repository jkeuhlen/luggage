package app

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestDefaultBinaryDir(t *testing.T) {
	t.Setenv("HOME", "/tmp/luggage-home")
	got, err := defaultBinaryDir()
	if err != nil {
		t.Fatalf("defaultBinaryDir error: %v", err)
	}
	want := filepath.Join("/tmp/luggage-home", ".local", "bin")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestInstallBinaryCopies(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-luggage")
	dst := filepath.Join(dir, "bin", "luggage")

	content := []byte("binary-content")
	if err := os.WriteFile(src, content, 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}

	installed, err := installBinary(src, dst)
	if err != nil {
		t.Fatalf("installBinary error: %v", err)
	}
	if !installed {
		t.Fatalf("expected installBinary to report installed=true")
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected %q, got %q", string(content), string(got))
	}
}

func TestInstallBinaryNoopWhenSamePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "luggage")
	if err := os.WriteFile(src, []byte("same"), 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}
	installed, err := installBinary(src, src)
	if err != nil {
		t.Fatalf("installBinary error: %v", err)
	}
	if installed {
		t.Fatalf("expected installed=false when src and dst are same")
	}
}
