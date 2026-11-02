package config

import (
	"fmt"
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

	key := t.TempDir() + "/key"
	_ = os.WriteFile(key, []byte("0123456789abcdef0123456789abcdef\n"), 0o600)
	if c, err = Parse([]string{"--data-dir", dir}, func(k string) string {
		return map[string]string{"DROPCATCH_SECRET_KEY_FILE": key}[k]
	}); err != nil || c.SecretKey != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("DROPCATCH_SECRET_KEY_FILE: %q, %v", c.SecretKey, err)
	}

	if c, err = Parse([]string{"--data-dir", dir, "--trusted-proxies", "172.16.0.0/12, 10.0.0.1,::ffff:192.0.2.1", "--allowed-hosts", "Dropcatch.Example., other.example"}, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(c.TrustedProxies) != "[172.16.0.0/12 10.0.0.1/32 192.0.2.1/32]" || fmt.Sprint(c.AllowedHosts) != "[dropcatch.example other.example]" {
		t.Fatalf("proxies %v, hosts %v", c.TrustedProxies, c.AllowedHosts)
	}

	bad := [][]string{
		{"--data-dir", ""},
		{"--data-dir", dir, "--username", "admin"},
		{"--data-dir", dir, "--password", "a", "--password-hash", "$2a$..."},
		{"--data-dir", dir, "--password-hash", "plain"},
		{"--data-dir", dir, "--password", "a", "--password-file", pw},
		{"--data-dir", dir, "--secret-file", dir + "/missing"},
		{"--data-dir", dir, "--apprise", dir + "/missing"},
		{"--data-dir", dir, "--secret-key", "too-short"},
		{"--data-dir", dir, "--hetzner-token", "removed"},
		{"--data-dir", dir, "--trusted-proxies", "traefik"},
	}
	for _, args := range bad {
		if _, err := Parse(args, func(string) string { return "" }); err == nil {
			t.Errorf("Parse(%v) succeeded", args)
		}
	}
}
