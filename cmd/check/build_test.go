// Build tests cover canonical output, entry refusals and tree comparisons.
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

func putFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	putFile(t, dir, "catalog.toml", []byte("[[catalog.sources]]\npublishers = [\"toshi\", \"acme\"]\nkeys = [\""+placeholderKey+"\"]\n"))
	putFile(t, dir, "entries/README.md", []byte("Entry files.\n"))
	return dir
}

func entryWithID(id, publisher string) string {
	e := strings.Replace(validEntry(), `"id":"widget"`, `"id":"`+id+`"`, 1)
	return strings.Replace(e, `"publisher":"toshi"`, `"publisher":"`+publisher+`"`, 1)
}

func runBuild(t *testing.T, dir string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir", dir, "--build"}, &stdout, &stderr, time.Time{}, nil)
	return code, stdout.String(), stderr.String()
}

func TestBuildEmptyAndUnchanged(t *testing.T) {
	dir := buildFixture(t)
	code, stdout, stderr := runBuild(t, dir)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("build: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	path := filepath.Join(dir, "index.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"entries\": [],\n  \"format\": 1,\n  \"key_id\": \"\"\n}\n"; string(first) != want {
		t.Fatalf("seed = %q, want %q", first, want)
	}
	code, stdout, stderr = runBuild(t, dir)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || stdout != "index.json already lists entries/: unchanged\n" || stderr != "" || !bytes.Equal(first, after) {
		t.Fatalf("second build: code %d, stdout %q, stderr %q, index %q", code, stdout, stderr, after)
	}
}

func TestBuildSortedCanonicalEntries(t *testing.T) {
	dir := buildFixture(t)
	alpha, zulu := entryWithID("alpha", "acme"), entryWithID("zulu", "toshi")
	putFile(t, dir, "entries/toshi/zulu.json", []byte(zulu))
	putFile(t, dir, "entries/acme/alpha.json", []byte(alpha))
	if code, out, err := runBuild(t, dir); code != 0 || out != "" || err != "" {
		t.Fatalf("build: code %d, stdout %q, stderr %q", code, out, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a, z interface{}
	if err := json.Unmarshal([]byte(alpha), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(zulu), &z); err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(map[string]interface{}{"entries": []interface{}{a, z}, "format": 1, "key_id": ""}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if !bytes.Equal(got, want) {
		t.Fatalf("built index:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildPreservesIndexMetadata(t *testing.T) {
	dir := buildFixture(t)
	putFile(t, dir, "entries/toshi/widget.json", []byte(validEntry()))
	putFile(t, dir, "index.json", []byte(`{"format":2,"sequence":9007199254740993,"expires":"2035-02-01T00:00:00Z","key_id":"key","entries":[]}`))
	if code, out, err := runBuild(t, dir); code != 0 || out != "" || err != "" {
		t.Fatalf("build: code %d, stdout %q, stderr %q", code, out, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"format": "2", "sequence": "9007199254740993", "expires": `"2035-02-01T00:00:00Z"`, "key_id": `"key"`} {
		if string(got[key]) != want {
			t.Errorf("%s = %s, want %s", key, got[key], want)
		}
	}
	var entries []entry
	if err := json.Unmarshal(got["entries"], &entries); err != nil || len(entries) != 1 || entries[0].ID != "widget" {
		t.Fatalf("entries = %#v, error %v", entries, err)
	}
}

func TestBuildRefusals(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"top-level file", map[string]string{"entries/foo.json": validEntry()}, []string{"check: entries/foo.json: must be a JSON file at entries/<publisher>/<id>.json"}},
		{"nested file", map[string]string{"entries/toshi/nested/widget.json": validEntry()}, []string{"check: entries/toshi/nested/widget.json: must be a JSON file at entries/<publisher>/<id>.json"}},
		{"wrong extension", map[string]string{"entries/toshi/widget.txt": validEntry()}, []string{"check: entries/toshi/widget.txt: must be a JSON file at entries/<publisher>/<id>.json"}},
		{"folder mismatch", map[string]string{"entries/acme/widget.json": validEntry()}, []string{`check: entries/acme/widget.json: publisher "toshi" does not match folder "acme"`}},
		{"id mismatch", map[string]string{"entries/toshi/other.json": validEntry()}, []string{`check: entries/toshi/other.json: id "widget" does not match file name "other.json"`}},
		{"invalid entries reported separately", map[string]string{
			"entries/toshi/widget.json": strings.Replace(validEntry(), `"name":"Widget"`, `"name":""`, 1),
			"entries/toshi/other.json":  strings.Replace(entryWithID("other", "toshi"), `"docs":"https://example.com/docs"`, `"docs":"http://example.com/docs"`, 1),
		}, []string{"check: entries/toshi/widget.json: entry widget: name: must be 1 to 64 bytes", "check: entries/toshi/other.json: entry other: docs: must be an https URL"}},
		{"unlisted publisher", map[string]string{"entries/external/widget.json": entryWithID("widget", "external")}, []string{`check: entries/external/widget.json: entry widget: publisher: publisher "external" is not listed in catalog.toml`}},
		{"duplicate across publishers", map[string]string{"entries/acme/widget.json": entryWithID("widget", "acme"), "entries/toshi/widget.json": validEntry()}, []string{`check: entries/toshi/widget.json: entry widget: id: duplicate id "widget"`}},
		{"malformed JSON", map[string]string{"entries/toshi/widget.json": `{} {}`}, []string{"check: entries/toshi/widget.json: invalid JSON: more than one JSON value"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := buildFixture(t)
			original := []byte(`{"format":2,"entries":[]}`)
			putFile(t, dir, "index.json", original)
			for path, data := range tc.files {
				putFile(t, dir, path, []byte(data))
			}
			code, stdout, stderr := runBuild(t, dir)
			if code != 1 || stdout != "" {
				t.Fatalf("build: code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr, want+"\n") {
					t.Errorf("stderr %q does not contain line %q", stderr, want)
				}
			}
			got, err := os.ReadFile(filepath.Join(dir, "index.json"))
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("refused build changed index: %q, %v", got, err)
			}
		})
	}
}

func TestBuildRefusesSymlink(t *testing.T) {
	dir := buildFixture(t)
	putFile(t, dir, "source.json", []byte(validEntry()))
	if err := os.MkdirAll(filepath.Join(dir, "entries", "toshi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "source.json"), filepath.Join(dir, "entries", "toshi", "widget.json")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runBuild(t, dir)
	if code != 1 || stdout != "" || stderr != "check: entries/toshi/widget.json: must be a regular file\n" {
		t.Fatalf("build: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.json")); !os.IsNotExist(err) {
		t.Fatalf("index after refused build: %v", err)
	}
}

func TestBuildDoesNotRewriteSignedIndex(t *testing.T) {
	dir := buildFixture(t)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	pub := private.Public().(ed25519.PublicKey)
	var value interface{}
	if err := json.Unmarshal([]byte(validEntry()), &value); err != nil {
		t.Fatal(err)
	}
	entryBytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, dir, "entries/toshi/widget.json", entryBytes)
	index := []byte(fmt.Sprintf("{\"key_id\":%q, \"entries\": [ %s ],\"expires\":\"2035-02-01T00:00:00Z\",\"sequence\":2,\"format\":2}", thumbprint(pub), validEntry()))
	putFile(t, dir, "index.json", index)
	sig := ed25519.Sign(private, index)
	putFile(t, dir, "index.json.sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"))
	path := filepath.Join(dir, "index.json")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runBuild(t, dir)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || stdout != "index.json already lists entries/: unchanged\n" || stderr != "" || !bytes.Equal(index, after) || !before.ModTime().Equal(info.ModTime()) || !ed25519.Verify(pub, after, sig) {
		t.Fatalf("build: code %d, stdout %q, stderr %q; index changed: %t", code, stdout, stderr, !bytes.Equal(index, after))
	}
}

func TestTreeEntryComparison(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, ed25519.SeedSize))
	pub := private.Public().(ed25519.PublicKey)
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	alpha, bravo, charlie := entryWithID("alpha", "toshi"), entryWithID("bravo", "toshi"), entryWithID("charlie", "toshi")
	var reformatted interface{}
	if err := json.Unmarshal([]byte(alpha), &reformatted); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(reformatted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, indexEntries string
		files              map[string]string
		missingTree        bool
		want               []string
		forbid             string
	}{
		{"equal with different JSON bytes", "[" + alpha + "]", map[string]string{"entries/toshi/alpha.json": string(canonical)}, false, nil, ""},
		{"missing extra and changed", "[" + bravo + "," + charlie + "]", map[string]string{"entries/toshi/alpha.json": alpha, "entries/toshi/charlie.json": strings.Replace(charlie, `"name":"Widget"`, `"name":"Other"`, 1)}, false, []string{"entries differ from entries/: run scripts/sign", "entry alpha: missing from index.json", "entry bravo: extra in index.json", "entry charlie: differs"}, ""},
		{"missing directory", "[]", nil, true, []string{"check: entries"}, ""},
		{"invalid tree is not a partial comparison", "[" + bravo + "]", map[string]string{"entries/toshi/alpha.json": strings.Replace(alpha, `"name":"Widget"`, `"name":""`, 1)}, false, []string{"check: entries/toshi/alpha.json: entry alpha: name: must be 1 to 64 bytes"}, "entries differ from entries/"},
		{"entry order", "[" + bravo + "," + alpha + "]", map[string]string{"entries/toshi/alpha.json": alpha, "entries/toshi/bravo.json": bravo}, false, []string{"entries differ from entries/: run scripts/sign", "entry order or multiplicity differs"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			config := fmt.Sprintf("[[catalog.sources]]\nname = \"catalog\"\nurl = \"https://example.com/index.json\"\nkeys = [%q]\npublishers = [\"toshi\"]\n", keyPrefix+base64.StdEncoding.EncodeToString(pub))
			index := []byte(fmt.Sprintf(`{"format":2,"sequence":1,"expires":"2035-02-01T00:00:00Z","key_id":%q,"entries":%s}`, thumbprint(pub), tc.indexEntries))
			sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, index)) + "\n")
			putFile(t, dir, "catalog.toml", []byte(config))
			putFile(t, dir, "index.json", index)
			putFile(t, dir, "index.json.sig", sig)
			if !tc.missingTree {
				putFile(t, dir, "entries/README.md", nil)
			}
			for path, data := range tc.files {
				putFile(t, dir, path, []byte(data))
			}
			writeGeneration(t, dir, 1, index, sig)
			var stdout, stderr bytes.Buffer
			code := run([]string{"--dir", dir}, &stdout, &stderr, now, nil)
			if len(tc.want) == 0 && (code != 0 || stderr.Len() != 0) {
				t.Fatalf("check: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
			if len(tc.want) > 0 && (code != 1 || stdout.Len() != 0) {
				t.Fatalf("check: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q does not contain %q", stderr.String(), want)
				}
			}
			if tc.forbid != "" && strings.Contains(stderr.String(), tc.forbid) {
				t.Errorf("stderr %q contains %q", stderr.String(), tc.forbid)
			}
		})
	}
}
