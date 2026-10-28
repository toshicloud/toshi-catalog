// Load reads each catalog file and collects its schema and reference problems.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type publishersFile struct {
	Publishers []Publisher `toml:"publishers"`
}

type builtinsFile struct {
	Builtins     []Builtin     `toml:"builtins"`
	KeyProviders []KeyProvider `toml:"key_providers"`
}

type distributionFile struct {
	Distribution struct {
		Name string `toml:"name"`
	} `toml:"distribution"`
	Products     []DistributionProduct     `toml:"products"`
	KeyProviders []DistributionKeyProvider `toml:"key_providers"`
	Modules      []Module                  `toml:"modules"`
}

// Load returns all readable records even when they contain schema problems.
// Filesystem failures are returned separately from invalid TOML and data.
func Load(dir string) (*Catalog, []Problem, error) {
	c := &Catalog{}
	var problems []Problem
	report := func(file string) reporter {
		return func(field, format string, args ...any) {
			problems = append(problems, Problem{file, field, fmt.Sprintf(format, args...)})
		}
	}
	read := func(file string, dst any) (bool, error) {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		if err != nil {
			return false, fmt.Errorf("%s: %w", file, err)
		}
		return decode(data, dst, report(file)), nil
	}

	var pf publishersFile
	if _, err := read("publishers.toml", &pf); err != nil {
		return nil, problems, err
	}
	c.Publishers = pf.Publishers
	validatePublishers(c.Publishers, report("publishers.toml"))

	productFiles := make(map[string]bool)
	productByFile := make(map[string]*Product)
	entries, err := os.ReadDir(filepath.Join(dir, "products"))
	if err != nil {
		return nil, problems, fmt.Errorf("products: %w", err)
	}
	for _, entry := range entries {
		file := "products/" + entry.Name()
		if entry.Name() == "README.md" && entry.Type().IsRegular() {
			continue
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".toml") {
			report(file)("file", "must be a regular products/<id>.toml file")
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".toml")
		productFiles[id] = true
		var p Product
		ok, err := read(file, &p)
		if err != nil {
			return nil, problems, err
		}
		if ok {
			p.file = file
			c.Products = append(c.Products, p)
		}
	}
	// Populate pointers after appending, so slice growth cannot invalidate them.
	for i := range c.Products {
		p := &c.Products[i]
		name := strings.TrimSuffix(filepath.Base(p.file), ".toml")
		productByFile[name] = p
	}
	validateProducts(c.Products, c.Publishers, report)

	var bf builtinsFile
	if _, err := read("builtins.toml", &bf); err != nil {
		return nil, problems, err
	}
	c.Builtins = bf.Builtins
	validateBuiltins(c.Builtins, productFiles, report("builtins.toml"))
	builtinStability := make(map[string]string)
	for _, b := range c.Builtins {
		builtinStability[b.ID] = b.Stability
	}
	c.KeyProviders = bf.KeyProviders
	validateKeyProviders(c.KeyProviders, report("builtins.toml"))
	keyProviderKinds := make(map[string]bool)
	for _, k := range c.KeyProviders {
		keyProviderKinds[k.Kind] = true
	}

	entries, err = os.ReadDir(filepath.Join(dir, "distributions"))
	if err != nil {
		return nil, problems, fmt.Errorf("distributions: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		file := "distributions/" + entry.Name()
		if !entry.Type().IsRegular() {
			report(file)("file", "must be a regular file")
			continue
		}
		var df distributionFile
		ok, err := read(file, &df)
		if err != nil {
			return nil, problems, err
		}
		if !ok {
			continue
		}
		d := Distribution{Name: df.Distribution.Name, Products: df.Products, KeyProviders: df.KeyProviders, Modules: df.Modules, File: file}
		validateDistribution(d, builtinStability, keyProviderKinds, productFiles, productByFile, report(file))
		sort.SliceStable(d.Products, func(i, j int) bool { return d.Products[i].ID < d.Products[j].ID })
		sort.SliceStable(d.KeyProviders, func(i, j int) bool { return d.KeyProviders[i].Kind < d.KeyProviders[j].Kind })
		sort.SliceStable(d.Modules, func(i, j int) bool {
			if d.Modules[i].Path != d.Modules[j].Path {
				return d.Modules[i].Path < d.Modules[j].Path
			}
			return d.Modules[i].Version < d.Modules[j].Version
		})
		c.Distributions = append(c.Distributions, d)
	}
	sort.SliceStable(c.Publishers, func(i, j int) bool { return c.Publishers[i].ID < c.Publishers[j].ID })
	sort.SliceStable(c.Builtins, func(i, j int) bool { return c.Builtins[i].ID < c.Builtins[j].ID })
	sort.SliceStable(c.KeyProviders, func(i, j int) bool { return c.KeyProviders[i].Kind < c.KeyProviders[j].Kind })
	sort.SliceStable(c.Products, func(i, j int) bool { return c.Products[i].ID < c.Products[j].ID })
	sort.SliceStable(c.Distributions, func(i, j int) bool {
		if c.Distributions[i].Name != c.Distributions[j].Name {
			return c.Distributions[i].Name < c.Distributions[j].Name
		}
		return c.Distributions[i].File < c.Distributions[j].File
	})
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		if problems[i].Field != problems[j].Field {
			return problems[i].Field < problems[j].Field
		}
		return problems[i].Message < problems[j].Message
	})
	unique := problems[:0]
	for _, p := range problems {
		if len(unique) == 0 || unique[len(unique)-1] != p {
			unique = append(unique, p)
		}
	}
	return c, unique, nil
}
