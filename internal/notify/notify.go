// Package notify sends notifications through the Apprise CLI, which
// supports most chat, push and e-mail services through one URL format
// (https://github.com/caronc/apprise/wiki).
package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

// Message types understood by Apprise (they pick an icon or colour).
const (
	Info    = "info"
	Success = "success"
	Warning = "warning"
	Failure = "failure"
)

var ErrUnavailable = errors.New("the apprise command is not installed")

type Apprise struct {
	Path string // empty when Apprise is not installed
}

// Send delivers one notification to the service at url.
func (a *Apprise) Send(ctx context.Context, url, title, body, kind string) error {
	return a.run(ctx, url, "-t", title, "-b", body, "-n", kind)
}

// Validate checks that Apprise understands url, without sending anything.
func (a *Apprise) Validate(ctx context.Context, url string) error {
	if err := CheckURL(url); err != nil {
		return err
	}
	if a.Path == "" {
		// Accept the URL now; sending reports the missing command.
		return nil
	}
	if err := a.run(ctx, url, "--dry-run", "-b", "test"); err != nil {
		return errors.New("Apprise does not recognise this URL")
	}
	return nil
}

// CheckURL rejects values that cannot be an Apprise URL.
func CheckURL(url string) error {
	if !strings.Contains(url, "://") {
		return errors.New("an Apprise URL looks like scheme://..., such as ntfy://topic or mailto://user:pass@example.com")
	}
	if strings.ContainsFunc(url, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return errors.New("the URL must not contain spaces or line breaks")
	}
	return nil
}

func (a *Apprise) run(ctx context.Context, url string, args ...string) error {
	if a.Path == "" {
		return ErrUnavailable
	}
	if err := CheckURL(url); err != nil {
		return err
	}
	// The URL usually contains a token or password: pass it in a private
	// file rather than as an argument, which other users can see in ps.
	f, err := os.CreateTemp("", "dropcatch-apprise-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(url + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	// Persistent storage is only a cache for some services; keep it in memory
	// so that Apprise needs no writable home directory.
	args = append([]string{"-v", "--storage-mode", "memory", "--config", f.Name()}, args...)
	cmd := exec.CommandContext(ctx, a.Path, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("apprise timed out")
		}
		return fmt.Errorf("apprise failed: %s", summary(out.String(), err))
	}
	return nil
}

// summary returns the most relevant line of Apprise's output.
func summary(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		for _, level := range []string{" - ERROR - ", " - WARNING - "} {
			if _, msg, ok := strings.Cut(l, level); ok && !strings.HasPrefix(msg, "Failed to load Apprise configuration") {
				return msg
			}
		}
	}
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}
