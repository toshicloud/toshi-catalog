// Entry tests exercise documented shape refusals through the signed checker.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type object = map[string]interface{}

func child(m object, key string) object { return m[key].(map[string]interface{}) }
func item(m object, key string, i int) object {
	return m[key].([]interface{})[i].(map[string]interface{})
}

func TestEntryRefusals(t *testing.T) {
	release := func(m object, i int) object { return item(m, "releases", i) }
	artifact := func(m object, i int) object { return item(release(m, 0), "artifacts", i) }
	withdrawn := func(m object) object { return item(m, "withdrawn", 0) }
	tests := []struct {
		name   string
		mutate func(object)
		want   string
		format int
	}{
		{"unknown entry", func(m object) { m["extra"] = true }, "extra: unknown field", 0},
		{"unknown surfaces", func(m object) { child(m, "surfaces")["extra"] = true }, "surfaces.extra: unknown field", 0},
		{"unknown runs", func(m object) { child(m, "runs")["extra"] = true }, "runs.extra: unknown field", 0},
		{"unknown release", func(m object) { release(m, 0)["extra"] = true }, "releases[0].extra: unknown field", 0},
		{"unknown api", func(m object) { child(release(m, 0), "api")["extra"] = true }, "releases[0].api.extra: unknown field", 0},
		{"unknown host", func(m object) { child(release(m, 0), "host")["extra"] = true }, "releases[0].host.extra: unknown field", 0},
		{"unknown artifact", func(m object) { artifact(m, 0)["extra"] = true }, "releases[0].artifacts[0].extra: unknown field", 0},
		{"unknown state", func(m object) { child(m, "state")["extra"] = true }, "state.extra: unknown field", 0},
		{"unknown withdrawn", func(m object) { withdrawn(m)["extra"] = true }, "withdrawn[0].extra: unknown field", 0},
		{"missing id", func(m object) { delete(m, "id") }, "id: invalid id", 0},
		{"missing name", func(m object) { delete(m, "name") }, "name: must be 1 to 64 bytes", 0},
		{"missing publisher", func(m object) { delete(m, "publisher") }, "publisher: invalid publisher", 0},
		{"missing entry version", func(m object) { delete(m, "version") }, "version: must be 1 to 64 bytes", 0},
		{"missing description", func(m object) { delete(m, "description") }, "description: must be 1 to 280 bytes", 0},
		{"missing runs", func(m object) { delete(m, "runs") }, "runs: is required", 0},
		{"missing where", func(m object) { delete(child(m, "runs"), "where") }, "runs: must be publisher", 0},
		{"missing service app", func(m object) { child(m, "runs")["where"] = "service" }, "runs: must be publisher", 0},
		{"missing docs", func(m object) { delete(m, "docs") }, "docs: must be an https URL", 0},
		{"missing release version", func(m object) { delete(release(m, 0), "version") }, "releases[0].version: is required", 0},
		{"missing api", func(m object) { delete(release(m, 0), "api") }, "releases[0].api: is required", 0},
		{"missing api min", func(m object) { delete(child(release(m, 0), "api"), "min") }, "releases[0].api.min: must be MAJOR.MINOR.PATCH", 0},
		{"missing host min", func(m object) { delete(child(release(m, 0), "host"), "min") }, "releases[0].host.min: must be MAJOR.MINOR.PATCH", 0},
		{"missing artifact kind", func(m object) { delete(artifact(m, 0), "kind") }, "releases[0].artifacts[0].kind: must be cli or image", 0},
		{"missing artifact os", func(m object) { delete(artifact(m, 0), "os") }, "releases[0].artifacts[0].os: is required", 0},
		{"missing artifact arch", func(m object) { delete(artifact(m, 0), "arch") }, "releases[0].artifacts[0].arch: is required", 0},
		{"missing artifact sha256", func(m object) { delete(artifact(m, 0), "sha256") }, "releases[0].artifacts[0].sha256: must be 64 lower-case hex digits", 0},
		{"missing artifact image", func(m object) { delete(artifact(m, 1), "image") }, "releases[0].artifacts[1].image: must be an image repository", 0},
		{"missing artifact digest", func(m object) { delete(artifact(m, 1), "digest") }, "releases[0].artifacts[1].digest: must be sha256:", 0},
		{"missing state word", func(m object) { delete(child(m, "state"), "state") }, "state.state: must be listed, deprecated or blocked", 0},
		{"missing withdrawn version", func(m object) { delete(withdrawn(m), "version") }, "withdrawn[0].version: is required", 0},
		{"missing withdrawn digests", func(m object) { delete(withdrawn(m), "digests") }, "withdrawn[0].digests: at least one digest is required", 0},
		{"missing withdrawn reason", func(m object) { delete(withdrawn(m), "reason") }, "withdrawn[0].reason: is required", 0},
		{"missing withdrawn effective", func(m object) { delete(withdrawn(m), "effective") }, "withdrawn[0].effective: must be RFC 3339", 0},
		{"non-https docs", func(m object) { m["docs"] = "http://example.com" }, "docs: must be an https URL", 0},
		{"non-https surface", func(m object) { child(m, "surfaces")["api"] = "http://example.com" }, "surfaces.api: must be an https URL", 0},
		{"empty sdk", func(m object) { child(m, "surfaces")["sdk"] = "" }, "surfaces.sdk: must not be empty", 0},
		{"bad state", func(m object) { child(m, "state")["state"] = "unknown" }, "state.state: must be listed, deprecated or blocked", 0},
		{"deprecated without reason", func(m object) { child(m, "state")["state"] = "deprecated" }, "state.reason: is required for deprecated", 0},
		{"blocked without reason", func(m object) { child(m, "state")["state"] = "blocked" }, "state.reason: is required for blocked", 0},
		{"default release missing", func(m object) { m["version"] = "3.0.0" }, "version: does not name a release", 0},
		{"duplicate release", func(m object) { release(m, 1)["version"] = "1.4.0" }, "releases[1].version: duplicate release version", 0},
		{"reversed api", func(m object) { child(release(m, 0), "api")["min"] = "0.5.0" }, "releases[0].api: min must not exceed max", 0},
		{"bad api max", func(m object) { child(release(m, 0), "api")["max"] = "1.0" }, "releases[0].api.max: must be MAJOR.MINOR.PATCH", 0},
		{"bad cli sha", func(m object) { artifact(m, 0)["sha256"] = strings.Repeat("A", 64) }, "releases[0].artifacts[0].sha256: must be 64 lower-case hex digits", 0},
		{"bad image digest", func(m object) { artifact(m, 1)["digest"] = "sha256:abcd" }, "releases[0].artifacts[1].digest: must be sha256:", 0},
		{"image tag", func(m object) { artifact(m, 1)["image"] = "registry.example:5000/widget:latest" }, "releases[0].artifacts[1].image: must be an image repository", 0},
		{"image digest in name", func(m object) { artifact(m, 1)["image"] = "registry.example/widget@sha256:abcd" }, "releases[0].artifacts[1].image: must be an image repository", 0},
		{"cli with image field", func(m object) { artifact(m, 0)["image"] = "widget" }, "releases[0].artifacts[0].image: is not allowed for cli", 0},
		{"image with cli field", func(m object) { artifact(m, 1)["os"] = "linux" }, "releases[0].artifacts[1].os: is not allowed for image", 0},
		{"withdrawn listed", func(m object) { withdrawn(m)["state"] = "listed" }, "withdrawn[0].state: must be deprecated or blocked", 0},
		{"bad withdrawn digest", func(m object) { withdrawn(m)["digests"] = []interface{}{"sha256:abc"} }, "withdrawn[0].digests[0]: must be a SHA-256 digest", 0},
		{"withdrawn and release", func(m object) { withdrawn(m)["version"] = "1.4.0" }, "withdrawn[0].version: version \"1.4.0\" is also in releases", 0},
		{"duplicate withdrawn", func(m object) { m["withdrawn"] = append(m["withdrawn"].([]interface{}), withdrawn(m)) }, "withdrawn[1].version: duplicate withdrawn version", 0},
		{"bad runs", func(m object) { child(m, "runs")["app"] = "app" }, "runs: must be publisher", 0},
		{"bad access", func(m object) { m["access"] = []interface{}{"jobs..read"} }, "access[0]: invalid access name", 0},
		{"duplicate access", func(m object) { m["access"] = []interface{}{"jobs.read", "jobs.read"} }, "access[1]: duplicate access name", 0},
		{"null optional field", func(m object) { m["surfaces"] = nil }, "surfaces: must not be null", 0},
		{"wrong nested type", func(m object) { child(m, "surfaces")["sdk"] = 3 }, "surfaces.sdk: must be a string", 0},
		{"format 1 state", func(m object) {}, "state: is not allowed in format 1", 1},
		{"format 1 releases", func(m object) {}, "releases: is not allowed in format 1", 1},
		{"format 1 withdrawn", func(m object) {}, "withdrawn: is not allowed in format 1", 1},
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	pub := private.Public().(ed25519.PublicKey)
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	config := fmt.Sprintf("[[catalog.sources]]\nname = \"catalog\"\nurl = \"https://example.com/index.json\"\nkeys = [%q]\npublishers = [\"toshi\"]\n", keyPrefix+base64.StdEncoding.EncodeToString(pub))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var m object
			if err := json.Unmarshal([]byte(validEntry()), &m); err != nil {
				t.Fatal(err)
			}
			tc.mutate(m)
			format := tc.format
			if format == 0 {
				format = 2
			}
			index, err := json.Marshal(map[string]interface{}{"format": format, "sequence": 1, "expires": "2035-02-01T00:00:00Z", "key_id": thumbprint(pub), "entries": []object{m}})
			if err != nil {
				t.Fatal(err)
			}
			sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, index)) + "\n")
			dir := t.TempDir()
			for name, data := range map[string][]byte{"catalog.toml": []byte(config), "index.json": index, "index.json.sig": sig} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeEntriesFromIndex(t, dir, index)
			writeGeneration(t, dir, 1, index, sig)
			var stdout, stderr bytes.Buffer
			code := run([]string{"--dir", dir}, &stdout, &stderr, now, nil)
			label := "widget"
			if _, ok := m["id"]; !ok {
				label = "entries[0]"
			}
			want := "check: index.json: entry " + label + ": " + tc.want
			if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), want) {
				t.Fatalf("code %d, stdout %q, stderr %q; want %q", code, stdout.String(), stderr.String(), want)
			}
		})
	}
}
