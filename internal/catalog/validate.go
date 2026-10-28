// Schema validation for publishers, products and their versions.
package catalog

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	cliPattern    = regexp.MustCompile(`^toshi-[a-z][a-z0-9-]*$`)
	modulePattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:/[A-Za-z0-9._~-]+)+$`)
)

func nonempty(s string) bool { return len(s) > 0 }

func httpsURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.Opaque == ""
}

func validatePublishers(publishers []Publisher, report reporter) {
	seen := make(map[string]bool)
	for i, p := range publishers {
		field := fmt.Sprintf("publishers[%d]", i)
		if !idPattern.MatchString(p.ID) {
			report(field+".id", "must start with a lowercase letter and contain only lowercase letters, digits and underscores")
		}
		if p.ID != "" && seen[p.ID] {
			report(field+".id", "duplicate publisher %q", p.ID)
		}
		seen[p.ID] = true
		if !nonempty(p.Name) {
			report(field+".name", "must not be empty")
		}
		if !httpsURL(p.URL) {
			report(field+".url", "must be an HTTPS URL")
		}
	}
}

func validateProducts(products []Product, publishers []Publisher, reporterFor func(string) reporter) {
	publisherIDs := make(map[string]bool)
	for _, p := range publishers {
		publisherIDs[p.ID] = true
	}
	ids, modules := make(map[string]string), make(map[string]string)
	for _, p := range products {
		report := reporterFor(p.file)
		if !idPattern.MatchString(p.ID) {
			report("id", "must start with a lowercase letter and contain only lowercase letters, digits and underscores")
		}
		name := strings.TrimSuffix(filepath.Base(p.file), ".toml")
		if p.ID != name {
			report("id", "must match file name %q", name)
		}
		if previous, exists := ids[p.ID]; p.ID != "" && exists {
			report("id", "duplicates %s", previous)
		} else if p.ID != "" {
			ids[p.ID] = p.file
		}
		if !nonempty(p.Name) || len(p.Name) > 64 {
			report("name", "must contain 1 to 64 bytes")
		}
		if !nonempty(p.Description) || len(p.Description) > 280 {
			report("description", "must contain 1 to 280 bytes")
		}
		if !idPattern.MatchString(p.Publisher) || !publisherIDs[p.Publisher] {
			report("publisher", "must name a listed publisher")
		}
		if !modulePattern.MatchString(p.Module) {
			report("module", "must be an unversioned Go module path with a lowercase dotted host")
		}
		if previous, exists := modules[p.Module]; p.Module != "" && exists {
			report("module", "duplicates %s", previous)
		} else if p.Module != "" {
			modules[p.Module] = p.file
		}
		if !httpsURL(p.Docs) {
			report("docs", "must be an HTTPS URL")
		}
		if p.Surfaces != nil {
			s := p.Surfaces
			if s.API != nil && !httpsURL(*s.API) {
				report("surfaces.api", "must be an HTTPS URL")
			}
			if s.CLI != nil && !cliPattern.MatchString(*s.CLI) {
				report("surfaces.cli", "must be a toshi- command name")
			}
			if s.Dashboard != nil && !httpsURL(*s.Dashboard) {
				report("surfaces.dashboard", "must be an HTTPS URL")
			}
			if s.SDK != nil && !nonempty(*s.SDK) {
				report("surfaces.sdk", "must not be empty")
			}
		}
		validateWithdrawal(p.Withdrawal, "withdrawn", report)
		if len(p.Versions) == 0 {
			report("versions", "must contain at least one version")
		}
		versions := make(map[string]bool)
		for i, v := range p.Versions {
			field := fmt.Sprintf("versions[%d]", i)
			valid := validVersion(v.Version)
			if !valid {
				report(field+".version", "must be a semantic version beginning with v")
			}
			if v.Version != "" && versions[v.Version] {
				report(field+".version", "duplicate version %q", v.Version)
			}
			versions[v.Version] = true
			switch v.Stability {
			case "alpha", "beta", "stable":
				if v.RemovedIn != nil {
					report(field+".removed_in", "is allowed only for a deprecated version")
				}
			case "deprecated":
				if v.RemovedIn == nil || !validVersion(*v.RemovedIn) {
					report(field+".removed_in", "must be a semantic version beginning with v")
				} else if valid && compareVersions(*v.RemovedIn, v.Version) <= 0 {
					report(field+".removed_in", "must be greater than the version")
				}
			default:
				report(field+".stability", "must be alpha, beta, stable or deprecated")
				if v.RemovedIn != nil {
					report(field+".removed_in", "is allowed only for a deprecated version")
				}
			}
			if v.Toshi == nil {
				report(field+".toshi", "is required")
			} else {
				minValid := validVersion(v.Toshi.Min)
				if !minValid {
					report(field+".toshi.min", "must be a semantic version beginning with v")
				}
				if v.Toshi.Max != nil {
					if !validVersion(*v.Toshi.Max) {
						report(field+".toshi.max", "must be a semantic version beginning with v")
					} else if minValid && compareVersions(v.Toshi.Min, *v.Toshi.Max) > 0 {
						report(field+".toshi.max", "must not be less than min")
					}
				}
			}
			validateWithdrawal(v.Withdrawal, field+".withdrawn", report)
		}
	}
}

func validateWithdrawal(w *Withdrawal, field string, report reporter) {
	if w == nil {
		return
	}
	if !nonempty(w.Reason) {
		report(field+".reason", "must not be empty")
	}
	if w.Since.Location().String() != "date-local" || w.Since.Hour() != 0 ||
		w.Since.Minute() != 0 || w.Since.Second() != 0 || w.Since.Nanosecond() != 0 {
		report(field+".since", "must be a TOML local date")
	}
}
