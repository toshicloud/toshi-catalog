// Entry validation checks the documented catalog entry and release shapes.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

type entry struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Publisher   string      `json:"publisher"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	Surfaces    *surfaces   `json:"surfaces"`
	Access      []string    `json:"access"`
	Runs        *runs       `json:"runs"`
	Docs        string      `json:"docs"`
	State       *state      `json:"state"`
	Releases    []release   `json:"releases"`
	Withdrawn   []withdrawn `json:"withdrawn"`
}

type surfaces struct {
	API       *string `json:"api"`
	CLI       *string `json:"cli"`
	Dashboard *string `json:"dashboard"`
	SDK       *string `json:"sdk"`
}

type runs struct {
	Where string  `json:"where"`
	App   *string `json:"app"`
}

type state struct {
	State     string  `json:"state"`
	Reason    string  `json:"reason"`
	Successor *string `json:"successor"`
}

type release struct {
	Version   string     `json:"version"`
	API       *apiRange  `json:"api"`
	Host      *hostRange `json:"host"`
	Artifacts []artifact `json:"artifacts"`
	State     *state     `json:"state"`
}

type apiRange struct {
	Min string  `json:"min"`
	Max *string `json:"max"`
}

type hostRange struct {
	Min string `json:"min"`
}

type artifact struct {
	Kind   string  `json:"kind"`
	OS     *string `json:"os"`
	Arch   *string `json:"arch"`
	SHA256 *string `json:"sha256"`
	Image  *string `json:"image"`
	Digest *string `json:"digest"`
}

type withdrawn struct {
	Version   string   `json:"version"`
	Digests   []string `json:"digests"`
	Reason    string   `json:"reason"`
	Effective string   `json:"effective"`
	Successor *string  `json:"successor"`
	State     *string  `json:"state"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	accessPattern  = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)
	versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	hexPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// shape describes the JSON types so all unknown keys and type errors can be
// reported, even though a strict decoder returns only its first error.
type shape struct {
	kind   string
	fields map[string]*shape
	item   *shape
}

var (
	strShape   = &shape{kind: "string"}
	stateShape = &shape{kind: "object", fields: map[string]*shape{
		"state": strShape, "reason": strShape, "successor": strShape,
	}}
	artifactShape = &shape{kind: "object", fields: map[string]*shape{
		"kind": strShape, "os": strShape, "arch": strShape, "sha256": strShape,
		"image": strShape, "digest": strShape,
	}}
	releaseShape = &shape{kind: "object", fields: map[string]*shape{
		"version":   strShape,
		"api":       {kind: "object", fields: map[string]*shape{"min": strShape, "max": strShape}},
		"host":      {kind: "object", fields: map[string]*shape{"min": strShape}},
		"artifacts": {kind: "array", item: artifactShape},
		"state":     stateShape,
	}}
	entryShape = &shape{kind: "object", fields: map[string]*shape{
		"id": strShape, "name": strShape, "publisher": strShape, "version": strShape,
		"description": strShape, "docs": strShape,
		"surfaces": {kind: "object", fields: map[string]*shape{
			"api": strShape, "cli": strShape, "dashboard": strShape, "sdk": strShape,
		}},
		"access": {kind: "array", item: strShape},
		"runs":   {kind: "object", fields: map[string]*shape{"where": strShape, "app": strShape}},
		"state":  stateShape, "releases": {kind: "array", item: releaseShape},
		"withdrawn": {kind: "array", item: &shape{kind: "object", fields: map[string]*shape{
			"version": strShape, "digests": {kind: "array", item: strShape}, "reason": strShape,
			"effective": strShape, "successor": strShape, "state": strShape,
		}}},
	}}
)

type entryProblem func(field, format string, args ...interface{})

func checkShape(raw json.RawMessage, path string, s *shape, problem entryProblem) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		problem(path, "must not be null")
		return
	}
	switch s.kind {
	case "string":
		var value string
		if json.Unmarshal(raw, &value) != nil {
			problem(path, "must be a string")
		}
	case "object":
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			problem(path, "must be an object")
			return
		}
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			field := name
			if path != "" {
				field = path + "." + name
			}
			child, ok := s.fields[name]
			if !ok {
				problem(field, "unknown field")
				continue
			}
			checkShape(fields[name], field, child, problem)
		}
	case "array":
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			problem(path, "must be an array")
			return
		}
		for i, item := range items {
			checkShape(item, fmt.Sprintf("%s[%d]", path, i), s.item, problem)
		}
	}
}

func httpsURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != ""
}

func compareVersions(a, b string) int {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, y := strings.TrimLeft(aa[i], "0"), strings.TrimLeft(bb[i], "0")
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		if x != y {
			return strings.Compare(x, y)
		}
	}
	return 0
}

func checkState(s *state, path string, problem entryProblem) {
	if s == nil {
		return
	}
	switch s.State {
	case "listed":
	case "deprecated", "blocked":
		if s.Reason == "" {
			problem(path+".reason", "is required for %s", s.State)
		}
	default:
		problem(path+".state", "must be listed, deprecated or blocked")
	}
}

func checkEntry(raw json.RawMessage, i, format int, publishers map[string]bool, checkPublishers bool, seen map[string]bool, report func(string, ...interface{})) {
	var e entry
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	err := dec.Decode(&e)
	label := e.ID
	if label == "" {
		label = fmt.Sprintf("entries[%d]", i)
	}
	problem := func(field, format string, args ...interface{}) {
		if field == "" {
			field = "object"
		}
		report("entry %s: %s: %s", label, field, fmt.Sprintf(format, args...))
	}
	checkShape(raw, "", entryShape, problem)
	if err != nil && !strings.HasPrefix(err.Error(), "json: unknown field ") {
		if _, ok := err.(*json.UnmarshalTypeError); !ok {
			problem("object", "%v", err)
		}
	}
	if !idPattern.MatchString(e.ID) {
		problem("id", "invalid id %q", e.ID)
	} else if seen[e.ID] {
		problem("id", "duplicate id %q", e.ID)
	}
	if e.ID != "" {
		seen[e.ID] = true
	}
	if len(e.Name) < 1 || len(e.Name) > 64 {
		problem("name", "must be 1 to 64 bytes")
	}
	if !idPattern.MatchString(e.Publisher) {
		problem("publisher", "invalid publisher %q", e.Publisher)
	}
	if e.Publisher != "" && checkPublishers && !publishers[e.Publisher] {
		problem("publisher", "publisher %q is not listed in catalog.toml", e.Publisher)
	}
	if len(e.Version) < 1 || len(e.Version) > 64 || strings.Contains(e.Version, " ") {
		problem("version", "must be 1 to 64 bytes with no spaces")
	}
	if len(e.Description) < 1 || len(e.Description) > 280 {
		problem("description", "must be 1 to 280 bytes")
	}
	if e.Surfaces != nil {
		for _, item := range []struct {
			name  string
			value *string
		}{{"api", e.Surfaces.API}, {"cli", e.Surfaces.CLI}, {"dashboard", e.Surfaces.Dashboard}} {
			if item.value != nil && !httpsURL(*item.value) {
				problem("surfaces."+item.name, "must be an https URL")
			}
		}
		if e.Surfaces.SDK != nil && *e.Surfaces.SDK == "" {
			problem("surfaces.sdk", "must not be empty")
		}
	}
	access := make(map[string]bool)
	for j, name := range e.Access {
		field := fmt.Sprintf("access[%d]", j)
		if !accessPattern.MatchString(name) {
			problem(field, "invalid access name %q", name)
		}
		if access[name] {
			problem(field, "duplicate access name %q", name)
		}
		access[name] = true
	}
	if e.Runs == nil {
		problem("runs", "is required")
	} else if !((e.Runs.Where == "publisher" && e.Runs.App == nil) || (e.Runs.Where == "service" && e.Runs.App != nil && *e.Runs.App != "")) {
		problem("runs", "must be publisher without app or service with a non-empty app")
	}
	if !httpsURL(e.Docs) {
		problem("docs", "must be an https URL")
	}
	if format == 1 {
		if e.State != nil {
			problem("state", "is not allowed in format 1")
		}
		if e.Releases != nil {
			problem("releases", "is not allowed in format 1")
		}
		if e.Withdrawn != nil {
			problem("withdrawn", "is not allowed in format 1")
		}
	}
	checkState(e.State, "state", problem)
	versions := make(map[string]bool)
	for j, r := range e.Releases {
		path := fmt.Sprintf("releases[%d]", j)
		if r.Version == "" {
			problem(path+".version", "is required")
		} else if versions[r.Version] {
			problem(path+".version", "duplicate release version %q", r.Version)
		}
		versions[r.Version] = true
		if r.API == nil {
			problem(path+".api", "is required")
		} else {
			minOK := versionPattern.MatchString(r.API.Min)
			if !minOK {
				problem(path+".api.min", "must be MAJOR.MINOR.PATCH")
			}
			if r.API.Max != nil {
				maxOK := versionPattern.MatchString(*r.API.Max)
				if !maxOK {
					problem(path+".api.max", "must be MAJOR.MINOR.PATCH")
				}
				if minOK && maxOK && compareVersions(r.API.Min, *r.API.Max) > 0 {
					problem(path+".api", "min must not exceed max")
				}
			}
		}
		if r.Host != nil && !versionPattern.MatchString(r.Host.Min) {
			problem(path+".host.min", "must be MAJOR.MINOR.PATCH")
		}
		for k, a := range r.Artifacts {
			checkArtifact(a, fmt.Sprintf("%s.artifacts[%d]", path, k), problem)
		}
		checkState(r.State, path+".state", problem)
	}
	if e.Releases != nil && !versions[e.Version] {
		problem("version", "does not name a release")
	}
	withdrawnVersions := make(map[string]bool)
	for j, w := range e.Withdrawn {
		path := fmt.Sprintf("withdrawn[%d]", j)
		if w.Version == "" {
			problem(path+".version", "is required")
		} else {
			if withdrawnVersions[w.Version] {
				problem(path+".version", "duplicate withdrawn version %q", w.Version)
			}
			if versions[w.Version] {
				problem(path+".version", "version %q is also in releases", w.Version)
			}
		}
		withdrawnVersions[w.Version] = true
		if len(w.Digests) == 0 {
			problem(path+".digests", "at least one digest is required")
		}
		for k, digest := range w.Digests {
			if !hexPattern.MatchString(strings.TrimPrefix(digest, "sha256:")) {
				problem(fmt.Sprintf("%s.digests[%d]", path, k), "must be a SHA-256 digest in lower-case hex")
			}
		}
		if w.Reason == "" {
			problem(path+".reason", "is required")
		}
		if _, err := time.Parse(time.RFC3339, w.Effective); err != nil {
			problem(path+".effective", "must be RFC 3339")
		}
		if w.State != nil && *w.State != "deprecated" && *w.State != "blocked" {
			problem(path+".state", "must be deprecated or blocked")
		}
	}
}

func checkArtifact(a artifact, path string, problem entryProblem) {
	switch a.Kind {
	case "cli":
		if a.OS == nil || *a.OS == "" {
			problem(path+".os", "is required")
		}
		if a.Arch == nil || *a.Arch == "" {
			problem(path+".arch", "is required")
		}
		if a.SHA256 == nil || !hexPattern.MatchString(*a.SHA256) {
			problem(path+".sha256", "must be 64 lower-case hex digits")
		}
		if a.Image != nil {
			problem(path+".image", "is not allowed for cli")
		}
		if a.Digest != nil {
			problem(path+".digest", "is not allowed for cli")
		}
	case "image":
		if a.Image == nil || *a.Image == "" || strings.Contains(*a.Image, "@") || strings.Contains((*a.Image)[strings.LastIndex(*a.Image, "/")+1:], ":") {
			problem(path+".image", "must be an image repository without a tag or digest")
		}
		if a.Digest == nil || !strings.HasPrefix(*a.Digest, "sha256:") || !hexPattern.MatchString(strings.TrimPrefix(*a.Digest, "sha256:")) {
			problem(path+".digest", "must be sha256: followed by 64 lower-case hex digits")
		}
		if a.OS != nil {
			problem(path+".os", "is not allowed for image")
		}
		if a.Arch != nil {
			problem(path+".arch", "is not allowed for image")
		}
		if a.SHA256 != nil {
			problem(path+".sha256", "is not allowed for image")
		}
	default:
		problem(path+".kind", "must be cli or image")
	}
}
