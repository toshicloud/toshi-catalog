// The check command validates a signed catalog index and its sequence.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	keyPrefix         = "ed25519:"
	placeholderKey    = "ed25519:REPLACE_WITH_THE_CATALOG_PUBLIC_KEY"
	maxIndexBytes     = 4 << 20
	maxSignatureBytes = 1 << 10
)

// thumbprint is the key ID an index names: the JWK thumbprint of the
// public key (RFC 7638 section 3), whose required members for an Ed25519
// key are crv, kty and x (RFC 8037 section 2), hashed with SHA-256 and
// written in base64url without padding.
func thumbprint(pub ed25519.PublicKey) string {
	// encoding/json writes struct fields in order and without whitespace,
	// which is the member order and form RFC 7638 requires.
	jwk, _ := json.Marshal(struct {
		Crv string `json:"crv"`
		Kty string `json:"kty"`
		X   string `json:"x"`
	}{"Ed25519", "OKP", base64.RawURLEncoding.EncodeToString(pub)})
	sum := sha256.Sum256(jwk)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// parseKey reads a key as catalog.toml lists it: "ed25519:" and the 32
// bytes of the public key in standard base64.
func parseKey(s string) (ed25519.PublicKey, error) {
	encoded, ok := strings.CutPrefix(s, keyPrefix)
	if !ok {
		return nil, fmt.Errorf("key %q does not start with %q", s, keyPrefix)
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("key %q is not standard base64", s)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("key %q holds %d bytes, not %d", s, len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now()))
}

func run(args []string, stdout, stderr io.Writer, now time.Time) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", ".", "catalog directory")
	base := flags.String("base", "", "earlier index")
	validFor := flags.Duration("valid-for", 0, "minimum remaining validity")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "check: flags: %v\n", err)
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "check: flags: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 1
	}

	problems := 0
	problem := func(file, format string, a ...interface{}) {
		fmt.Fprintf(stderr, "check: %s: %s\n", file, fmt.Sprintf(format, a...))
		problems++
	}
	read := func(path, label string, max int64) ([]byte, bool) {
		f, err := os.Open(path)
		if err != nil {
			problem(label, "%v", err)
			return nil, false
		}
		defer f.Close()
		var r io.Reader = f
		if max > 0 {
			r = io.LimitReader(f, max+1)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			problem(label, "%v", err)
			return nil, false
		}
		if max > 0 && int64(len(b)) > max {
			problem(label, "exceeds %d bytes", max)
			return nil, false
		}
		return b, true
	}

	var cfg struct {
		Catalog struct {
			Sources []struct {
				Name       string   `toml:"name"`
				URL        string   `toml:"url"`
				Keys       []string `toml:"keys"`
				Publishers []string `toml:"publishers"`
			} `toml:"sources"`
		} `toml:"catalog"`
	}
	var keys []ed25519.PublicKey
	publishers := make(map[string]bool)
	configReady := false
	if b, ok := read(filepath.Join(*dir, "catalog.toml"), "catalog.toml", 0); ok {
		meta, err := toml.Decode(string(b), &cfg)
		if err != nil {
			problem("catalog.toml", "%v", err)
		} else {
			for _, key := range meta.Undecoded() {
				problem("catalog.toml", "unknown key %s", key)
			}
			if len(cfg.Catalog.Sources) != 1 {
				problem("catalog.toml", "want exactly one [[catalog.sources]]")
			}
			configReady = len(cfg.Catalog.Sources) == 1
			for i, source := range cfg.Catalog.Sources {
				if source.Name == "" {
					problem("catalog.toml", "sources[%d]: name is required", i)
				}
				if source.URL == "" {
					problem("catalog.toml", "sources[%d]: url is required", i)
				}
				if len(source.Keys) == 0 {
					problem("catalog.toml", "sources[%d]: at least one key is required", i)
				}
				if len(source.Publishers) == 0 {
					problem("catalog.toml", "sources[%d]: at least one publisher is required", i)
				}
				for _, key := range source.Keys {
					if key == placeholderKey {
						problem("catalog.toml", "keys holds the placeholder: the key holder sets the catalog's public key and signs index.json (see README.md)")
						continue
					}
					pub, err := parseKey(key)
					if err != nil {
						problem("catalog.toml", "%v", err)
					} else if configReady {
						keys = append(keys, pub)
					}
				}
				if configReady {
					for _, publisher := range source.Publishers {
						publishers[publisher] = true
					}
				}
			}
		}
	}

	type indexData struct {
		Format   int               `json:"format"`
		Sequence int64             `json:"sequence"`
		Expires  *time.Time        `json:"expires"`
		KeyID    string            `json:"key_id"`
		Entries  []json.RawMessage `json:"entries"`
	}
	var ix indexData
	index, indexRead := read(filepath.Join(*dir, "index.json"), "index.json", maxIndexBytes)
	indexParsed := false
	if indexRead {
		dec := json.NewDecoder(bytes.NewReader(index))
		dec.DisallowUnknownFields()
		decodeErr := dec.Decode(&ix)
		if decodeErr != nil {
			// A strict decode reports only one unknown field. Report each one,
			// then retain valid fields so independent checks can still run.
			var fields map[string]json.RawMessage
			var unknown []string
			if err := json.NewDecoder(bytes.NewReader(index)).Decode(&fields); err == nil {
				for name := range fields {
					known := false
					for _, expected := range []string{"format", "sequence", "expires", "key_id", "entries"} {
						known = known || strings.EqualFold(name, expected)
					}
					if !known {
						unknown = append(unknown, name)
					}
				}
			}
			sort.Strings(unknown)
			for _, name := range unknown {
				problem("index.json", "json: unknown field %q", name)
			}
			strictErr := decodeErr
			if len(unknown) == 0 || !strings.HasPrefix(strictErr.Error(), "json: unknown field ") {
				problem("index.json", "%v", strictErr)
			}
			ix = indexData{}
			dec = json.NewDecoder(bytes.NewReader(index))
			decodeErr = dec.Decode(&ix)
			if decodeErr != nil && decodeErr.Error() != strictErr.Error() {
				problem("index.json", "%v", decodeErr)
			}
		}
		if decodeErr == nil {
			indexParsed = true
			if dec.More() {
				problem("index.json", "more than one JSON value")
			} else {
				var extra json.RawMessage
				if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
					problem("index.json", "unexpected data after JSON value")
				}
			}
		}
	}
	if indexParsed {
		if ix.Format != 2 {
			problem("index.json", "format must be 2")
		}
		if ix.Sequence < 1 {
			problem("index.json", "sequence must be at least 1")
		}
		if ix.Expires == nil {
			problem("index.json", "expires is required")
		} else {
			expires := ix.Expires.UTC().Format(time.RFC3339)
			if !now.Before(*ix.Expires) {
				problem("index.json", "expired at %s: run scripts/sign", expires)
			} else if *validFor > 0 && ix.Expires.Sub(now) < *validFor {
				problem("index.json", "expires at %s, within %s: run scripts/sign", expires, *validFor)
			}
		}
		if ix.KeyID == "" {
			problem("index.json", "key_id is required")
		}
		validName := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
		seen := make(map[string]bool)
		for i, raw := range ix.Entries {
			var entry struct {
				ID        string `json:"id"`
				Publisher string `json:"publisher"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				problem("index.json", "entries[%d]: %v", i, err)
				continue
			}
			if !validName.MatchString(entry.ID) {
				problem("index.json", "entries[%d]: invalid id %q", i, entry.ID)
			}
			if !validName.MatchString(entry.Publisher) {
				problem("index.json", "entries[%d]: invalid publisher %q", i, entry.Publisher)
			}
			if configReady && len(publishers) > 0 && !publishers[entry.Publisher] {
				problem("index.json", "entries[%d]: publisher %q is not listed in catalog.toml", i, entry.Publisher)
			}
			if seen[entry.ID] {
				problem("index.json", "entries[%d]: duplicate id %q", i, entry.ID)
			}
			seen[entry.ID] = true
		}
	}

	signature, sigRead := read(filepath.Join(*dir, "index.json.sig"), "index.json.sig", maxSignatureBytes)
	var sig []byte
	sigParsed := false
	if sigRead {
		var err error
		sig, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
		if err != nil {
			problem("index.json.sig", "invalid base64 signature: %v", err)
		} else {
			sigParsed = true
		}
	}
	if indexParsed && ix.KeyID != "" && configReady && len(keys) > 0 {
		var trusted ed25519.PublicKey
		for _, key := range keys {
			if thumbprint(key) == ix.KeyID {
				trusted = key
				break
			}
		}
		if trusted == nil {
			problem("index.json", "signed with key %q, which this source does not trust", ix.KeyID)
		} else if sigParsed && !ed25519.Verify(trusted, index, sig) {
			problem("index.json.sig", "the signature does not verify with key %q", ix.KeyID)
		}
	}

	if *base != "" {
		if b, ok := read(*base, *base, maxIndexBytes); ok {
			var prior struct {
				Format   int   `json:"format"`
				Sequence int64 `json:"sequence"`
			}
			dec := json.NewDecoder(bytes.NewReader(b))
			err := dec.Decode(&prior)
			if err == nil && dec.More() {
				err = errors.New("more than one JSON value")
			} else if err == nil {
				var extra json.RawMessage
				if end := dec.Decode(&extra); !errors.Is(end, io.EOF) {
					err = errors.New("unexpected data after JSON value")
				}
			}
			if err == nil {
				switch prior.Format {
				case 1:
					prior.Sequence = 0
				case 2:
					if prior.Sequence < 1 {
						err = errors.New("format 2 needs a sequence of at least 1")
					}
				default:
					err = fmt.Errorf("unknown format %d", prior.Format)
				}
			}
			if err != nil {
				problem(*base, "%v", err)
			} else if indexParsed {
				switch {
				case ix.Sequence < prior.Sequence:
					problem("index.json", "sequence %d is lower than main's %d", ix.Sequence, prior.Sequence)
				case ix.Sequence == prior.Sequence && !bytes.Equal(index, b):
					problem("index.json", "sequence %d is main's with other bytes: raise the sequence (scripts/sign does)", ix.Sequence)
				}
			}
		}
	}

	if problems > 0 {
		return 1
	}
	fmt.Fprintf(stdout, "index.json: sequence %d, key %s, expires %s, %d entries: ok\n", ix.Sequence, ix.KeyID, ix.Expires.UTC().Format(time.RFC3339), len(ix.Entries))
	return 0
}
