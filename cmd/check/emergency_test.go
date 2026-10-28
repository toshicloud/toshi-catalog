// Emergency rehearsal signs a block and checks that older indexes are refused.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmergencyBlockRehearsal(t *testing.T) {
	for _, kind := range []string{"withdrawn release", "entry"} {
		t.Run(kind, func(t *testing.T) {
			private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
			pub := private.Public().(ed25519.PublicKey)
			now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
			const reason = "Compromised signing key."
			var listed map[string]interface{}
			if err := json.Unmarshal([]byte(validEntry()), &listed); err != nil {
				t.Fatal(err)
			}
			listed["releases"] = listed["releases"].([]interface{})[:1]
			delete(listed, "withdrawn")
			original, err := json.Marshal(listed)
			if err != nil {
				t.Fatal(err)
			}
			var blocked map[string]interface{}
			if err := json.Unmarshal(original, &blocked); err != nil {
				t.Fatal(err)
			}
			if kind == "withdrawn release" {
				delete(blocked, "releases")
				blocked["withdrawn"] = []interface{}{map[string]interface{}{
					"version": "1.4.0", "digests": []string{"sha256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("b", 64)},
					"reason": reason, "effective": "2035-01-01T00:00:00Z", "state": "blocked",
				}}
			} else {
				blocked["state"] = map[string]interface{}{"state": "blocked", "reason": reason}
			}
			blockedEntry, err := json.Marshal(blocked)
			if err != nil {
				t.Fatal(err)
			}
			makeIndex := func(sequence int, raw []byte) []byte {
				return []byte(fmt.Sprintf(`{"format":2,"sequence":%d,"expires":"2035-02-01T00:00:00Z","key_id":%q,"entries":[%s]}`, sequence, thumbprint(pub), raw))
			}
			sign := func(index []byte) []byte {
				return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, index)) + "\n")
			}
			first, second := makeIndex(1, original), makeIndex(2, blockedEntry)
			firstSig, secondSig := sign(first), sign(second)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/index.json":
					_, _ = w.Write(first)
				case "/index.json.sig":
					_, _ = w.Write(firstSig)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			config := fmt.Sprintf("[[catalog.sources]]\nname = \"catalog\"\nurl = %q\nkeys = [%q]\npublishers = [\"toshi\"]\n", server.URL+"/index.json", keyPrefix+base64.StdEncoding.EncodeToString(pub))
			putFile(t, dir, "catalog.toml", []byte(config))
			publish := func(sequence int64, index, sig, raw []byte) {
				t.Helper()
				putFile(t, dir, "index.json", index)
				putFile(t, dir, "index.json.sig", sig)
				putFile(t, dir, "entries/toshi/widget.json", raw)
				writeGeneration(t, dir, sequence, index, sig)
			}
			check := func(args ...string) (int, string, string) {
				t.Helper()
				var stdout, stderr bytes.Buffer
				code := run(append([]string{"--dir", dir}, args...), &stdout, &stderr, now, server.Client())
				return code, stdout.String(), stderr.String()
			}
			publish(1, first, firstSig, original)
			if code, out, err := check(); code != 0 || err != "" {
				t.Fatalf("sequence 1: code %d, stdout %q, stderr %q", code, out, err)
			}
			publish(2, second, secondSig, blockedEntry)
			if code, out, err := check(); code != 0 || err != "" {
				t.Fatalf("sequence 2: code %d, stdout %q, stderr %q", code, out, err)
			}
			var decoded struct {
				Entries []entry `json:"entries"`
			}
			published, err := os.ReadFile(filepath.Join(dir, "index.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(published, &decoded); err != nil || len(decoded.Entries) != 1 {
				t.Fatalf("decode sequence 2: entries %#v, error %v", decoded.Entries, err)
			}
			got := decoded.Entries[0]
			if kind == "withdrawn release" {
				if len(got.Releases) != 0 || len(got.Withdrawn) != 1 || got.Withdrawn[0].State == nil || *got.Withdrawn[0].State != "blocked" || got.Withdrawn[0].Reason != reason || got.Withdrawn[0].Successor != nil || len(got.Withdrawn[0].Digests) != 2 {
					t.Fatalf("withdrawn block = %#v", got)
				}
			} else if got.State == nil || got.State.State != "blocked" || got.State.Reason != reason || got.State.Successor != nil {
				t.Fatalf("entry block = %#v", got.State)
			}
			putFile(t, dir, "base.json", second)
			publish(1, first, firstSig, original)
			if code, out, err := check("--base", filepath.Join(dir, "base.json")); code != 1 || out != "" || !strings.Contains(err, "sequence 1 is lower than") || strings.Count(err, "check:") != 1 {
				t.Fatalf("rollback against base: code %d, stdout %q, stderr %q", code, out, err)
			}
			publish(2, second, secondSig, blockedEntry)
			if code, out, err := check("--served"); code != 1 || out != "" || !strings.Contains(err, "served sequence 1 is lower than the committed 2") || strings.Count(err, "check:") != 1 {
				t.Fatalf("served rollback: code %d, stdout %q, stderr %q", code, out, err)
			}
		})
	}
}
