// Build and compare the index entries from the per-publisher entry files.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type reporter func(file, format string, args ...interface{})

// decodeJSON keeps numbers exact and rejects more than one JSON value.
func decodeJSON(data []byte) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("more than one JSON value")
		}
		return nil, err
	}
	return value, nil
}

func entriesFromTree(dir string, publishers map[string]bool, checkPublishers bool, report reporter) ([]interface{}, bool) {
	type namedEntry struct {
		id    string
		value interface{}
	}
	var found []namedEntry
	seen := make(map[string]bool)
	ok := true
	problem := func(file, format string, args ...interface{}) {
		ok = false
		report(file, format, args...)
	}
	root := filepath.Join(dir, "entries")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			problem("entries/", "%v", err)
			return nil
		}
		label := filepath.ToSlash(rel)
		if walkErr != nil {
			problem(label, "%v", walkErr)
			return nil
		}
		if path == root {
			if !d.IsDir() {
				problem("entries/", "must be a directory")
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if label == "entries/README.md" {
			return nil
		}
		parts := strings.Split(label, "/")
		if len(parts) != 3 || !strings.HasSuffix(parts[2], ".json") {
			problem(label, "must be a JSON file at entries/<publisher>/<id>.json")
			return nil
		}
		if !d.Type().IsRegular() {
			problem(label, "must be a regular file")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			problem(label, "%v", err)
			return nil
		}
		value, err := decodeJSON(data)
		if err != nil {
			problem(label, "invalid JSON: %v", err)
			return nil
		}
		var identity struct {
			ID        string `json:"id"`
			Publisher string `json:"publisher"`
		}
		_ = json.Unmarshal(data, &identity)
		checkEntry(data, len(found), 2, publishers, checkPublishers, seen, func(format string, args ...interface{}) {
			problem(label, format, args...)
		})
		if identity.Publisher != parts[1] {
			problem(label, "publisher %q does not match folder %q", identity.Publisher, parts[1])
		}
		if identity.ID != strings.TrimSuffix(parts[2], ".json") {
			problem(label, "id %q does not match file name %q", identity.ID, parts[2])
		}
		found = append(found, namedEntry{identity.ID, value})
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return found[i].id < found[j].id })
	entries := make([]interface{}, 0, len(found))
	for _, e := range found {
		entries = append(entries, e.value)
	}
	return entries, ok
}

func writeBuiltIndex(dir string, entries []interface{}, stdout io.Writer, report reporter) bool {
	path := filepath.Join(dir, "index.json")
	data, err := os.ReadFile(path)
	var fields map[string]json.RawMessage
	switch {
	case errors.Is(err, os.ErrNotExist):
		fields = map[string]json.RawMessage{"format": json.RawMessage("1"), "key_id": json.RawMessage(`""`)}
	case err != nil:
		report("index.json", "%v", err)
		return false
	default:
		if err := json.Unmarshal(data, &fields); err != nil {
			report("index.json", "%v", err)
			return false
		}
		if fields == nil {
			report("index.json", "must be a JSON object")
			return false
		}
		if raw, exists := fields["entries"]; exists {
			current, err := decodeJSON(raw)
			if err == nil && reflect.DeepEqual(current, entries) {
				fmt.Fprintln(stdout, "index.json already lists entries/: unchanged")
				return true
			}
		}
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		report("index.json", "%v", err)
		return false
	}
	fields["entries"] = encoded
	encoded, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		report("index.json", "%v", err)
		return false
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		report("index.json", "%v", err)
		return false
	}
	return true
}

func compareEntries(index []json.RawMessage, tree []interface{}, report reporter) {
	actual := make([]interface{}, 0, len(index))
	for _, raw := range index {
		value, err := decodeJSON(raw)
		if err != nil {
			report("index.json", "entries: %v", err)
			return
		}
		actual = append(actual, value)
	}
	if reflect.DeepEqual(actual, tree) {
		return
	}
	report("index.json", "entries differ from entries/: run scripts/sign")
	byID := func(entries []interface{}) map[string]interface{} {
		result := make(map[string]interface{})
		for _, value := range entries {
			if fields, ok := value.(map[string]interface{}); ok {
				if id, ok := fields["id"].(string); ok && id != "" {
					result[id] = value
				}
			}
		}
		return result
	}
	want, got := byID(tree), byID(actual)
	ids := make(map[string]bool)
	for id := range want {
		ids[id] = true
	}
	for id := range got {
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	details := 0
	for _, id := range ordered {
		w, inTree := want[id]
		g, inIndex := got[id]
		switch {
		case !inIndex:
			report("index.json", "entry %s: missing from index.json", id)
		case !inTree:
			report("index.json", "entry %s: extra in index.json", id)
		case !reflect.DeepEqual(w, g):
			report("index.json", "entry %s: differs", id)
		default:
			continue
		}
		details++
	}
	if details == 0 {
		report("index.json", "entry order or multiplicity differs")
	}
}
