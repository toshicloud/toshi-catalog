// Command tests check exit codes, output streams and the directory flag.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"products", "distributions"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "publishers.toml"), []byte("[[publishers]]\nid = \"toshi\"\nname = \"Toshi\"\nurl = \"https://example.com\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builtins.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "distributions", "hosted.toml"), []byte("[distribution]\nname = \"hosted\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--dir", dir}, &stdout, &stderr); code != 0 || stdout.String() != "catalog: 1 publishers, 0 builtins, 0 products, 1 distributions: ok\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--dir", dir, "unexpected"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "check: flags:") || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "publishers.toml"), []byte("[[publishers]]\nid = \"bad id\"\nname = \"\"\nurl = \"http://example.com\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--dir", dir}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "publishers[0].id:") || !strings.Contains(stderr.String(), "publishers[0].url:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
