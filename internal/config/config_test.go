package config

import (
	"os"
	"testing"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"DROPCATCH_DATA_DIR": dir, "DROPCATCH_LISTEN": ":9000"}
	c, err := Parse([]string{"--base-path", "dropcatch/"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != dir || c.Listen != ":9000" || c.BasePath != "/dropcatch/" || c.AuthEnabled() {
		t.Fatalf("config = %+v", c)
	}

	// A flag overrides the environment.
	if c, err = Parse([]string{"--listen", ":7000"}, func(k string) string { return env[k] }); err != nil || c.Listen != ":7000" {
		t.Fatalf("--listen: %+v, %v", c, err)
	}

	// Secrets can be read from files, without the trailing newline.
	pw := t.TempDir() + "/password"
	_ = os.WriteFile(pw, []byte("s3cret\n"), 0o600)
	if c, err = Parse([]string{"--data-dir", dir}, func(k string) string {
		return map[string]string{"DROPCATCH_PASSWORD_FILE": pw}[k]
	}); err != nil || c.Password != "s3cret" || !c.AuthEnabled() {
		t.Fatalf("DROPCATCH_PASSWORD_FILE: %q, %v", c.Password, err)
	}

	bad := [][]string{
		{"--data-dir", ""},
		{"--data-dir", dir, "--username", "admin"},
		{"--data-dir", dir, "--password", "a", "--password-hash", "$2a$..."},
		{"--data-dir", dir, "--password-hash", "plain"},
		{"--data-dir", dir, "--password", "a", "--password-file", pw},
		{"--data-dir", dir, "--secret-file", dir + "/missing"},
		{"--data-dir", dir, "--apprise", dir + "/missing"},
	}
	for _, args := range bad {
		if _, err := Parse(args, func(string) string { return "" }); err == nil {
			t.Errorf("Parse(%v) succeeded", args)
		}
	}
}
