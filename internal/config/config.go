// Package config parses Dropcatch configuration from CLI flags and
// DROPCATCH_* environment variables. Flags take precedence over environment
// variables.
//
// These are deployment settings. Settings the user changes in the web UI
// (check interval, notifications) live in the state file (package store).
package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	DataDir  string
	Listen   string
	BasePath string
	Apprise  string // empty when the Apprise CLI is unavailable
	// RDAPBootstrap is the URL of the RDAP bootstrap file, which lists
	// the RDAP server of each TLD.
	RDAPBootstrap string
	// OVHAPI replaces the OVH API host (for the fake registry); empty
	// uses the host of each subsidiary.
	OVHAPI string
	// HetznerToken is a Hetzner Cloud API token, needed to watch Hetzner
	// server types; HetznerAPI replaces the API's base URL.
	HetznerToken string
	HetznerAPI   string

	// Authentication is enabled when Password or PasswordHash is set.
	Username     string
	Password     string
	PasswordHash string // bcrypt
	Secret       string // session signing key; generated when empty
}

// AuthEnabled reports whether a password is required.
func (c *Config) AuthEnabled() bool { return c.Password != "" || c.PasswordHash != "" }

// Parse parses args (without the program name), falling back to env for unset flags.
func Parse(args []string, getenv func(string) string) (*Config, error) {
	c := &Config{}
	fs := flag.NewFlagSet("dropcatch", flag.ContinueOnError)

	env := func(name, def string) string {
		if v := getenv("DROPCATCH_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))); v != "" {
			return v
		}
		return def
	}

	fs.StringVar(&c.DataDir, "data-dir", env("data-dir", defaultDataDir()), "directory for the domain list, settings and session key")
	fs.StringVar(&c.Listen, "listen", env("listen", ":8080"), "address to listen on")
	fs.StringVar(&c.BasePath, "base-path", env("base-path", "/"), "URL path prefix when served behind a reverse proxy")
	apprise := fs.String("apprise", env("apprise", ""), "path to the apprise CLI (auto-detected on PATH when empty)")
	fs.StringVar(&c.RDAPBootstrap, "rdap-bootstrap", env("rdap-bootstrap", "https://data.iana.org/rdap/dns.json"), "URL of the RDAP bootstrap file (RFC 9224)")
	fs.StringVar(&c.OVHAPI, "ovh-api", env("ovh-api", ""), "base URL of the OVH API, such as https://eu.api.ovh.com/v1 (default: the API of each subsidiary)")
	fs.StringVar(&c.HetznerToken, "hetzner-token", env("hetzner-token", ""), "Hetzner Cloud API token (read-only is enough), to watch Hetzner Cloud servers")
	fs.StringVar(&c.HetznerAPI, "hetzner-api", env("hetzner-api", "https://api.hetzner.cloud/v1"), "base URL of the Hetzner Cloud API")
	fs.StringVar(&c.Username, "username", env("username", ""), "username for login (optional; requires a password)")
	fs.StringVar(&c.Password, "password", env("password", ""), "password for login; enables authentication")
	fs.StringVar(&c.PasswordHash, "password-hash", env("password-hash", ""), "bcrypt hash of the password (alternative to --password)")
	fs.StringVar(&c.Secret, "secret", env("secret", ""), "key for signing session cookies (default: generated and stored in the data dir)")
	secretFiles := map[string]*string{"password": &c.Password, "password-hash": &c.PasswordHash, "secret": &c.Secret, "hetzner-token": &c.HetznerToken}
	fileFlags := map[string]*string{}
	for name := range secretFiles {
		fileFlags[name] = fs.String(name+"-file", env(name+"-file", ""), "file containing the --"+name+" value (e.g. a Docker secret)")
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	// Command-line arguments are visible to every user on the host (ps, /proc).
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "password" || f.Name == "secret" || f.Name == "hetzner-token" {
			slog.Warn("--" + f.Name + " is visible in the process list; prefer DROPCATCH_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_")) + " or --" + f.Name + "-file")
		}
	})
	for name, file := range fileFlags {
		if *file == "" {
			continue
		}
		if *secretFiles[name] != "" {
			return nil, fmt.Errorf("set either --%s or --%s-file, not both", name, name)
		}
		b, err := os.ReadFile(*file)
		if err != nil {
			return nil, fmt.Errorf("reading --%s-file: %w", name, err)
		}
		// Files usually end with a newline that is not part of the value.
		if *secretFiles[name] = strings.TrimRight(string(b), "\r\n"); *secretFiles[name] == "" {
			return nil, fmt.Errorf("--%s-file %q is empty", name, *file)
		}
	}

	if c.DataDir == "" {
		return nil, errors.New("--data-dir (or DROPCATCH_DATA_DIR) is required")
	}
	var err error
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return nil, err
	}

	c.BasePath = "/" + strings.Trim(c.BasePath, "/")
	if c.BasePath != "/" {
		c.BasePath += "/"
	}

	if c.Password != "" && c.PasswordHash != "" {
		return nil, errors.New("set either --password or --password-hash, not both")
	}
	if c.Username != "" && !c.AuthEnabled() {
		return nil, errors.New("--username requires --password or --password-hash")
	}
	if c.PasswordHash != "" && !strings.HasPrefix(c.PasswordHash, "$2") {
		return nil, errors.New("--password-hash must be a bcrypt hash")
	}

	c.Apprise = *apprise
	if c.Apprise == "" {
		c.Apprise, _ = exec.LookPath("apprise")
	} else if _, err := exec.LookPath(c.Apprise); err != nil {
		return nil, fmt.Errorf("apprise not found at %q", c.Apprise)
	}

	return c, nil
}

func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "dropcatch")
	}
	return ""
}
