// Package catalog loads the product directory and deployment distributions.
package catalog

import (
	"fmt"
	"time"
)

// Catalog is the directory and its distributions, ordered by ID or name.
type Catalog struct {
	Publishers    []Publisher
	Products      []Product
	Distributions []Distribution
}

type Publisher struct {
	ID   string `toml:"id"`
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

type Product struct {
	ID          string      `toml:"id"`
	Name        string      `toml:"name"`
	Description string      `toml:"description"`
	Publisher   string      `toml:"publisher"`
	Module      string      `toml:"module"`
	Docs        string      `toml:"docs"`
	Surfaces    *Surfaces   `toml:"surfaces"`
	Withdrawal  *Withdrawal `toml:"withdrawn"`
	Versions    []Version   `toml:"versions"`
	file        string
}

type Surfaces struct {
	API       *string `toml:"api"`
	CLI       *string `toml:"cli"`
	Dashboard *string `toml:"dashboard"`
	SDK       *string `toml:"sdk"`
}

type Withdrawal struct {
	Reason string    `toml:"reason"`
	Since  time.Time `toml:"since"`
}

// Date returns the local date as it is displayed in the directory.
func (w Withdrawal) Date() string { return w.Since.Format("2006-01-02") }

type Version struct {
	Version    string         `toml:"version"`
	Stability  string         `toml:"stability"`
	RemovedIn  *string        `toml:"removed_in"`
	Toshi      *Compatibility `toml:"toshi"`
	Withdrawal *Withdrawal    `toml:"withdrawn"`
}

type Compatibility struct {
	Min string  `toml:"min"`
	Max *string `toml:"max"`
}

func (p Product) IsWithdrawn() bool { return p.Withdrawal != nil }
func (v Version) IsWithdrawn() bool { return v.Withdrawal != nil }

// Latest returns the highest non-withdrawn semantic version, or nil.
func (p *Product) Latest() *Version {
	var latest *Version
	for i := range p.Versions {
		v := &p.Versions[i]
		if v.IsWithdrawn() || !validVersion(v.Version) {
			continue
		}
		if latest == nil || compareVersions(v.Version, latest.Version) > 0 ||
			(compareVersions(v.Version, latest.Version) == 0 && v.Version > latest.Version) {
			latest = v
		}
	}
	return latest
}

type Distribution struct {
	Name     string
	Products []DistributionProduct
	Modules  []Module
	File     string
}

type DistributionProduct struct {
	ID      string `toml:"id"`
	Builtin *bool  `toml:"builtin"`
}

type Module struct {
	Path    string `toml:"path"`
	Version string `toml:"version"`
}

// Hosted returns a catalog product's module and active version in hosted.toml.
// Builtins and invalid or ambiguous references are not catalog products.
func (c *Catalog) Hosted(id string) (module, version string, ok bool) {
	var product *Product
	for i := range c.Products {
		if c.Products[i].ID == id {
			if product != nil {
				return "", "", false
			}
			product = &c.Products[i]
		}
	}
	if product == nil || product.IsWithdrawn() {
		return "", "", false
	}
	for i := range c.Products {
		if &c.Products[i] != product && c.Products[i].Module == product.Module {
			return "", "", false
		}
	}
	var hosted *Distribution
	for i := range c.Distributions {
		if c.Distributions[i].File != "distributions/hosted.toml" || c.Distributions[i].Name != "hosted" {
			continue
		}
		if hosted != nil {
			return "", "", false
		}
		hosted = &c.Distributions[i]
	}
	if hosted == nil {
		return "", "", false
	}
	listed := false
	for _, p := range hosted.Products {
		if p.ID == id {
			if listed || p.Builtin != nil {
				return "", "", false
			}
			listed = true
		}
	}
	var match *Module
	for i := range hosted.Modules {
		if hosted.Modules[i].Path == product.Module {
			if match != nil {
				return "", "", false
			}
			match = &hosted.Modules[i]
		}
	}
	if !listed || match == nil || !modulePattern.MatchString(match.Path) || !validVersion(match.Version) {
		return "", "", false
	}
	versions := 0
	for _, v := range product.Versions {
		if v.Version == match.Version {
			if v.IsWithdrawn() {
				return "", "", false
			}
			versions++
		}
	}
	if versions != 1 {
		return "", "", false
	}
	return match.Path, match.Version, true
}

// Problem describes a schema or cross-reference error in one file.
type Problem struct {
	File    string
	Field   string
	Message string
}

func (p Problem) String() string {
	return fmt.Sprintf("check: %s: %s: %s", p.File, p.Field, p.Message)
}
