// Served tests check the fetched pair and its relation to the committed index.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServed(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize))
	pub := private.Public().(ed25519.PublicKey)
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	index := func(sequence int) []byte {
		return []byte(fmt.Sprintf(`{"format":2,"sequence":%d,"expires":"2035-02-01T00:00:00Z","key_id":%q,"entries":[]}`, sequence, thumbprint(pub)))
	}
	sign := func(b []byte) []byte {
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, b)) + "\n")
	}
	tests := []struct {
		name                         string
		served, signature, committed []byte
		status                       int
		signatureStatus              int
		redirect                     bool
		sameHostRedirect             bool
		httpURL                      bool
		networkFailure               bool
		placeholder                  bool
		validFor                     string
		code                         int
		want                         string
		requests                     int32
	}{
		{name: "served ok without tree", served: index(2), code: 0, requests: 2},
		{name: "served ok with tree and no archive", served: index(2), committed: index(2), code: 0, requests: 2},
		{name: "served newer", served: index(2), committed: index(1), code: 0, requests: 2},
		{name: "not found", served: index(2), status: 404, code: 3, want: "check: fetch failed:", requests: 1},
		{name: "signature not found", served: index(2), signatureStatus: 404, code: 3, want: "index.json.sig: HTTP 404 Not Found", requests: 2},
		{name: "non-https URL", served: index(2), httpURL: true, code: 3, want: "URL must be https", requests: 0},
		{name: "network failure", served: index(2), networkFailure: true, code: 3, want: "check: fetch failed:", requests: 0},
		{name: "bad signature", served: index(2), signature: sign(index(1)), code: 1, want: "signature does not verify", requests: 2},
		{name: "untrusted key", served: bytes.Replace(index(2), []byte(thumbprint(pub)), []byte("unknown"), 1), code: 1, want: "which this source does not trust", requests: 2},
		{name: "expired", served: bytes.Replace(index(2), []byte("2035-02-01"), []byte("2034-02-01"), 1), code: 1, want: "expired at", requests: 2},
		{name: "bad entry", served: bytes.Replace(index(2), []byte(`"entries":[]`), []byte(`"entries":[{"id":"widget","publisher":"toshi"}]`), 1), code: 1, want: "entry widget: docs: must be an https URL", requests: 2},
		{name: "lower sequence", served: index(1), committed: index(2), code: 1, want: "served sequence 1 is lower than the committed 2", requests: 2},
		{name: "same sequence other bytes", served: append(index(2), ' '), committed: index(2), code: 1, want: "served sequence 2 is the committed sequence with other bytes", requests: 2},
		{name: "other host redirect", served: index(2), redirect: true, code: 3, want: "redirect to another host", requests: 1},
		{name: "same host redirect", served: index(2), sameHostRedirect: true, code: 0, requests: 3},
		{name: "placeholder before fetch", served: index(2), placeholder: true, code: 1, want: "keys holds the placeholder", requests: 0},
		{name: "valid-for", served: index(2), validFor: "800h", code: 1, want: "within 800h0m0s", requests: 2},
		{name: "oversized served index", served: bytes.Repeat([]byte("a"), maxIndexBytes+1), code: 3, want: "exceeds 4194304 bytes", requests: 1},
		{name: "oversized served signature", served: index(2), signature: bytes.Repeat([]byte("a"), maxSignatureBytes+1), code: 3, want: "exceeds 1024 bytes", requests: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			sig := tc.signature
			if sig == nil {
				sig = sign(tc.served)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tc.redirect {
					http.Redirect(w, r, "https://other.example/index.json", http.StatusFound)
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				if tc.sameHostRedirect && r.URL.Path == "/index.json" {
					http.Redirect(w, r, "/next", http.StatusFound)
					return
				}
				if tc.signatureStatus != 0 && r.URL.Path == "/index.json.sig" {
					w.WriteHeader(tc.signatureStatus)
					return
				}
				switch r.URL.Path {
				case "/index.json", "/next":
					_, _ = w.Write(tc.served)
				case "/index.json.sig":
					_, _ = w.Write(sig)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			key := keyPrefix + base64.StdEncoding.EncodeToString(pub)
			if tc.placeholder {
				key = placeholderKey
			}
			address := server.URL + "/index.json"
			if tc.httpURL {
				address = strings.Replace(address, "https://", "http://", 1)
			}
			config := fmt.Sprintf("[[catalog.sources]]\nname = \"catalog\"\nurl = %q\nkeys = [%q]\npublishers = [\"toshi\"]\n", address, key)
			if err := os.WriteFile(filepath.Join(dir, "catalog.toml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.committed != nil {
				if err := os.WriteFile(filepath.Join(dir, "index.json"), tc.committed, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.networkFailure {
				server.Close()
			}
			args := []string{"--dir", dir, "--served"}
			if tc.validFor != "" {
				args = append(args, "--valid-for", tc.validFor)
			}
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr, now, server.Client())
			if code != tc.code || (tc.want != "" && !strings.Contains(stderr.String(), tc.want)) || (code == 0 && stderr.Len() != 0) {
				t.Fatalf("code %d, stdout %q, stderr %q; want code %d and %q", code, stdout.String(), stderr.String(), tc.code, tc.want)
			}
			if got := requests.Load(); got != tc.requests {
				t.Errorf("requests %d, want %d", got, tc.requests)
			}
		})
	}
}
