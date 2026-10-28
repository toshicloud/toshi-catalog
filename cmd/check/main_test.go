// Tests exercise catalog checks and the signer's public-key derivation.
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	expiry := now.Add(240 * time.Hour).Format(time.RFC3339)
	line := keyPrefix + base64.StdEncoding.EncodeToString(pub)
	config := fmt.Sprintf("[[catalog.sources]]\nname = %q\nurl = %q\nkeys = [%q]\npublishers = [%q]\n", "catalog", "catalog", line, "toshi")
	index := func(format, sequence int, expires, key, entries string) []byte {
		t.Helper()
		expiresField := ""
		if expires != "" {
			expiresField = fmt.Sprintf(",\"expires\":%q", expires)
		}
		return []byte(fmt.Sprintf(`{"format":%d,"sequence":%d%s,"key_id":%q,"entries":%s}`, format, sequence, expiresField, key, entries))
	}
	entries := `[{"id":"widget","publisher":"toshi","future_field":{"anything":true}}]`
	valid := index(2, 3, expiry, KeyID(pub), entries)

	type fixture struct {
		config    string
		index     []byte
		signed    []byte
		signature []byte
		base      []byte
		hasBase   bool
		remove    []string
		validFor  string
	}
	cases := []struct {
		name   string
		mutate func(*fixture)
		want   []string
	}{
		{name: "valid"},
		{name: "expiry reported in UTC", mutate: func(f *fixture) { f.index = index(2, 3, "2035-01-11T05:00:00+05:00", KeyID(pub), entries) }},
		{name: "placeholder", mutate: func(f *fixture) { f.config = strings.ReplaceAll(f.config, line, placeholderKey) }, want: []string{"keys holds the placeholder: the key holder sets the catalog's public key and signs index.json (see README.md)"}},
		{name: "malformed key", mutate: func(f *fixture) { f.config = strings.ReplaceAll(f.config, line, "ed25519:bad") }, want: []string{"not 32 bytes of base64"}},
		{name: "missing catalog", mutate: func(f *fixture) { f.remove = []string{"catalog.toml"} }, want: []string{"check: catalog.toml:"}},
		{name: "missing index", mutate: func(f *fixture) { f.remove = []string{"index.json"} }, want: []string{"check: index.json:"}},
		{name: "missing signature", mutate: func(f *fixture) { f.remove = []string{"index.json.sig"} }, want: []string{"check: index.json.sig:"}},
		{name: "all files missing", mutate: func(f *fixture) { f.remove = []string{"catalog.toml", "index.json", "index.json.sig"} }, want: []string{"check: catalog.toml:", "check: index.json:", "check: index.json.sig:"}},
		{name: "unknown catalog key", mutate: func(f *fixture) { f.config += "extra = true\n" }, want: []string{"unknown key"}},
		{name: "unknown top-level field", mutate: func(f *fixture) { f.index = []byte(strings.TrimSuffix(string(f.index), "}") + `,"other":true}`) }, want: []string{"unknown field"}},
		{name: "unknown fields and other problems", mutate: func(f *fixture) {
			f.index = index(2, 0, "", KeyID(pub), `[{"id":"Bad-id","publisher":"other"}]`)
			f.index = []byte(strings.TrimSuffix(string(f.index), "}") + `,"first_unknown":true,"second_unknown":true}`)
			f.signature = []byte("invalid!")
		}, want: []string{"unknown field \"first_unknown\"", "unknown field \"second_unknown\"", "sequence must be at least 1", "expires is required", "invalid id", "publisher \"other\" is not listed", "invalid base64 signature"}},
		{name: "second JSON value", mutate: func(f *fixture) { f.index = append(f.index, []byte(" {}")...) }, want: []string{"more than one JSON value"}},
		{name: "trailing JSON delimiter", mutate: func(f *fixture) { f.index = append(f.index, ']') }, want: []string{"unexpected data after JSON value"}},
		{name: "format 1", mutate: func(f *fixture) { f.index = index(1, 3, expiry, KeyID(pub), entries) }, want: []string{"format must be 2"}},
		{name: "sequence 0", mutate: func(f *fixture) { f.index = index(2, 0, expiry, KeyID(pub), entries) }, want: []string{"sequence must be at least 1"}},
		{name: "no expires", mutate: func(f *fixture) { f.index = index(2, 3, "", KeyID(pub), entries) }, want: []string{"expires is required"}},
		{name: "untrusted key", mutate: func(f *fixture) { f.index = index(2, 3, expiry, KeyID(other), entries) }, want: []string{"which this source does not trust"}},
		{name: "signature over other bytes", mutate: func(f *fixture) { f.signed = f.index; f.index = append(bytes.Clone(f.index), ' ') }, want: []string{"signature does not verify"}},
		{name: "bad base64 signature", mutate: func(f *fixture) { f.signature = []byte("not base64!") }, want: []string{"invalid base64 signature"}},
		{name: "oversized signature", mutate: func(f *fixture) { f.signature = bytes.Repeat([]byte("a"), maxSignatureBytes+1) }, want: []string{"exceeds 1024 bytes"}},
		{name: "duplicate id", mutate: func(f *fixture) {
			f.index = index(2, 3, expiry, KeyID(pub), `[{"id":"widget","publisher":"toshi"},{"id":"widget","publisher":"toshi"}]`)
		}, want: []string{"duplicate id"}},
		{name: "bad id", mutate: func(f *fixture) { f.index = index(2, 3, expiry, KeyID(pub), `[{"id":"Bad-id","publisher":"toshi"}]`) }, want: []string{"invalid id"}},
		{name: "publisher not listed", mutate: func(f *fixture) { f.index = index(2, 3, expiry, KeyID(pub), `[{"id":"widget","publisher":"other"}]`) }, want: []string{"publisher \"other\" is not listed"}},
		{name: "base lower", mutate: func(f *fixture) { f.hasBase = true; f.base = []byte(`{"format":2,"sequence":4}`) }, want: []string{"sequence 3 is lower than main's 4"}},
		{name: "base equal same", mutate: func(f *fixture) { f.hasBase = true; f.base = bytes.Clone(f.index) }},
		{name: "base equal different", mutate: func(f *fixture) { f.hasBase = true; f.base = append(bytes.Clone(f.index), ' ') }, want: []string{"sequence 3 is main's with other bytes: raise the sequence (scripts/sign does)"}},
		{name: "base format 1", mutate: func(f *fixture) { f.hasBase = true; f.base = []byte(`{"format":1,"entries":[]}`) }},
		{name: "bad base", mutate: func(f *fixture) { f.hasBase = true; f.base = []byte(`{"format":2,"sequence":`) }, want: []string{"base.json:"}},
		{name: "expired", mutate: func(f *fixture) {
			f.index = index(2, 3, now.Format(time.RFC3339), KeyID(pub), entries)
			f.validFor = "168h"
		}, want: []string{"expired at 2035-01-01T00:00:00Z: run scripts/sign"}},
		{name: "within valid-for", mutate: func(f *fixture) { f.validFor = "241h" }, want: []string{"expires at 2035-01-11T00:00:00Z, within 241h0m0s: run scripts/sign"}},
		{name: "equal valid-for", mutate: func(f *fixture) { f.validFor = "240h" }},
		{name: "oversized index", mutate: func(f *fixture) { f.index = append(bytes.Clone(f.index), bytes.Repeat([]byte(" "), maxIndexBytes)...) }, want: []string{"exceeds 4194304 bytes"}},
		{name: "several independent problems", mutate: func(f *fixture) {
			f.config = strings.ReplaceAll(f.config, line, placeholderKey)
			f.index = index(2, 0, "", KeyID(pub), `[{"id":"Bad-id","publisher":"other"}]`)
			f.signature = []byte("invalid!")
		}, want: []string{"keys holds the placeholder", "sequence must be at least 1", "expires is required", "invalid id", "publisher \"other\" is not listed", "invalid base64 signature"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture{config: config, index: bytes.Clone(valid)}
			if tc.mutate != nil {
				tc.mutate(&f)
			}
			signed := f.index
			if f.signed != nil {
				signed = f.signed
			}
			sig := f.signature
			if sig == nil {
				sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, signed)) + "\n")
			}
			dir := t.TempDir()
			for name, data := range map[string][]byte{"catalog.toml": []byte(f.config), "index.json": f.index, "index.json.sig": sig} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range f.remove {
				if err := os.Remove(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"--dir", dir}
			if f.hasBase {
				base := filepath.Join(dir, "base.json")
				if err := os.WriteFile(base, f.base, 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--base", base)
			}
			if f.validFor != "" {
				args = append(args, "--valid-for", f.validFor)
			}
			var stdout, stderr bytes.Buffer
			got := run(args, &stdout, &stderr, now)
			if len(tc.want) == 0 {
				want := fmt.Sprintf("index.json: sequence 3, key %s, expires %s, 1 entries: ok\n", KeyID(pub), expiry)
				if got != 0 || stdout.String() != want || stderr.Len() != 0 {
					t.Fatalf("run: code %d, stdout %q, stderr %q; want code 0 and stdout %q", got, stdout.String(), stderr.String(), want)
				}
				return
			}
			if got != 1 || stdout.Len() != 0 {
				t.Fatalf("run: code %d, stdout %q, stderr %q; want code 1 and no stdout", got, stdout.String(), stderr.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q does not contain %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestSignerKeyDerivation(t *testing.T) {
	for _, tool := range []string{"bash", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `source "$1"; public_key_line "$2"; key_id "$2"`, "bash", filepath.Join("..", "..", "scripts", "sign"), key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("derive key: %v: %s", err, out)
	}
	want := keyPrefix + base64.StdEncoding.EncodeToString(pub) + "\n" + KeyID(pub) + "\n"
	if string(out) != want {
		t.Fatalf("derive key: got %q, want %q", out, want)
	}
}

func TestSignerRejectsInvalidKey(t *testing.T) {
	for _, tool := range []string{"bash", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	other, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(other)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"invalid PEM":    []byte("not a key"),
		"wrong key type": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
	} {
		t.Run(name, func(t *testing.T) {
			key := filepath.Join(t.TempDir(), "key.pem")
			if err := os.WriteFile(key, content, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, function := range []string{"public_key_line", "key_id"} {
				cmd := exec.Command("bash", "-c", `source "$1"; "$2" "$3"`, "bash", filepath.Join("..", "..", "scripts", "sign"), function, key)
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if err := cmd.Run(); err == nil || stdout.Len() != 0 {
					t.Errorf("%s: got error %v and stdout %q; want failure and no stdout", function, err, stdout.String())
				}
				if strings.Contains(stderr.String(), "PRIVATE KEY") {
					t.Errorf("%s printed private key material", function)
				}
			}
		})
	}
}

func TestCatalogKeyStatus(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := keyPrefix + base64.StdEncoding.EncodeToString(pub)
	tests := []struct {
		name, config, status, want string
	}{
		{"placeholder", "[[catalog.sources]]\nkeys = [\"" + placeholderKey + "\"]\n", "set", "[[catalog.sources]]\nkeys = [\"" + key + "\"]\n"},
		{"listed key", "[[catalog.sources]]\nkeys = [\n  \"" + key + "\",\n]\n", "present", ""},
		{"another key", "[[catalog.sources]]\nkeys = [\"ed25519:11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=\"]\n", "missing", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "catalog.toml")
			if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", `source scripts/sign; catalog_key_status "$1" "$2"`, "bash", path, key)
			cmd.Dir = filepath.Join("..", "..")
			cmd.Env = append(os.Environ(), "TMPDIR="+dir)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != tc.status {
				t.Fatalf("catalog key status: got %q, error %v; want %q", out, err, tc.status)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if want == "" {
				want = tc.config
			}
			if string(after) != want {
				t.Errorf("catalog changed to %q; want %q", after, want)
			}
		})
	}
}
