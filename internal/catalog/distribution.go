// Distribution validation ties listed products to their exact active versions.
package catalog

import "fmt"

func validateDistribution(d Distribution, builtins, keyProviders, files map[string]bool, products map[string]*Product, report reporter) {
	if !idPattern.MatchString(d.Name) {
		report("distribution.name", "must start with a lowercase letter and contain only lowercase letters, digits and underscores")
	}
	modules := make(map[string][]Module)
	for _, m := range d.Modules {
		modules[m.Path] = append(modules[m.Path], m)
	}
	seen := make(map[string]bool)
	owned := make(map[string]bool)
	for _, listed := range d.Products {
		field := "product " + listed.ID
		if listed.ID == "" {
			field = "product <empty>"
		}
		if !idPattern.MatchString(listed.ID) {
			report(field, "id must start with a lowercase letter and contain only lowercase letters, digits and underscores")
		}
		if seen[listed.ID] {
			report(field, "is listed more than once")
		}
		seen[listed.ID] = true
		if listed.Builtin != nil {
			if !*listed.Builtin {
				report(field, "a catalog product must omit builtin; a builtin must be true")
				continue
			}
			if files[listed.ID] {
				report(field, "builtin must not have a product entry")
			} else if !builtins[listed.ID] {
				report(field, "builtin requires a matching builtins.toml entry")
			}
			continue
		}
		p := products[listed.ID]
		if p == nil || p.ID != listed.ID {
			report(field, "requires a matching products/%s.toml entry", listed.ID)
			continue
		}
		if p.IsWithdrawn() {
			report(field, "product is withdrawn")
		}
		owned[p.Module] = true
		matches := modules[p.Module]
		if len(matches) != 1 {
			report(field, "requires exactly one module %q; found %d", p.Module, len(matches))
		}
		for _, m := range matches {
			found, withdrawn := false, false
			for _, v := range p.Versions {
				if v.Version == m.Version {
					found = true
					withdrawn = withdrawn || v.IsWithdrawn()
				}
			}
			if !found {
				report(field, "module version %q is not listed in the product", m.Version)
			} else if withdrawn {
				report(field, "module version %q is withdrawn", m.Version)
			}
		}
	}
	kinds := make(map[string]bool)
	for _, listed := range d.KeyProviders {
		field := "key provider " + listed.Kind
		if listed.Kind == "" {
			field = "key provider <empty>"
		}
		if !idPattern.MatchString(listed.Kind) {
			report(field, "kind must start with a lowercase letter and contain only lowercase letters, digits and underscores")
		}
		if kinds[listed.Kind] {
			report(field, "is listed more than once")
		}
		kinds[listed.Kind] = true
		if listed.Builtin == nil || !*listed.Builtin {
			report(field, "must be builtin = true: every key provider is part of Toshi itself")
		} else if !keyProviders[listed.Kind] {
			report(field, "builtin requires a matching builtins.toml key_providers entry")
		}
	}
	for i, m := range d.Modules {
		if !owned[m.Path] {
			field := fmt.Sprintf("module %s", m.Path)
			if m.Path == "" {
				field = fmt.Sprintf("modules[%d]", i)
			}
			report(field, "does not belong to a listed catalog product")
		}
	}
}
