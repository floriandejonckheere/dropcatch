package notify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeApprise writes a script that logs its arguments and the URL in its
// config file, and fails for URLs starting with "bad://".
func fakeApprise(t *testing.T) (*Apprise, string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    --config) url=$(cat "$2"); shift ;;
    *) echo "arg: $1" >> "` + log + `" ;;
  esac
  shift
done
echo "url: $url" >> "` + log + `"
case "$url" in bad://*) echo "2026-10-24 12:00:00,000 - ERROR - Unparseable URL $url" >&2; exit 1 ;; esac
`
	path := filepath.Join(dir, "apprise")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Apprise{Path: path}, log
}

func TestSend(t *testing.T) {
	a, log := fakeApprise(t)
	ctx := context.Background()
	if err := a.Send(ctx, "ntfy://topic", "Title", "Body text", Success); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"url: ntfy://topic", "arg: Title", "arg: Body text", "arg: success"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("apprise log lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "arg: ntfy://topic") {
		t.Error("the URL was passed as an argument")
	}

	err := a.Send(ctx, "bad://x", "T", "B", Info)
	if err == nil || !strings.Contains(err.Error(), "Unparseable URL bad://x") {
		t.Fatalf("failing send: %v", err)
	}
	if err := a.Validate(ctx, "bad://x"); err == nil {
		t.Fatal("bad URL validated")
	}
	if err := a.Validate(ctx, "ntfy://topic"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckURL(t *testing.T) {
	for _, u := range []string{"", "ntfy", "ntfy://a b", "ntfy://a\nmailto://b"} {
		if CheckURL(u) == nil {
			t.Errorf("CheckURL(%q) accepted", u)
		}
	}
	if err := (&Apprise{}).Send(context.Background(), "ntfy://topic", "T", "B", Info); err != ErrUnavailable {
		t.Errorf("Send without apprise: %v", err)
	}
}
