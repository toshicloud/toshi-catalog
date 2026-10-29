// Catalog tests cover schema errors, cross-references and withdrawal transitions.
package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const publishersFixture = `[[publishers]]
id = "acme"
name = "Acme"
url = "https://example.com/"
`

const productFixture = `id = "deploys"
name = "Deploys"
description = "Deploy applications."
publisher = "acme"
module = "github.com/acme/deploys"
docs = "https://example.com/docs"

[surfaces]
api = "https://example.com/api"
cli = "toshi-deploys"
dashboard = "https://example.com/dashboard"
sdk = "github.com/acme/deploys-go"

[[versions]]
version = "v1.4.0"
stability = "beta"
toshi = { min = "v0.6.0", max = "v0.7.0" }
`

const distributionFixture = `[distribution]
name = "hosted"
[[products]]
id = "runners"
builtin = true
stability = "stable"
[[products]]
id = "deploys"
stability = "beta"
[[key_providers]]
kind = "transit"
builtin = true
[[object_storage]]
kind = "s3"
builtin = true
[[modules]]
path = "github.com/acme/deploys"
version = "v1.4.0"
`

const builtinsFixture = `[[builtins]]
id = "runners"
name = "Runners"
description = "Runs jobs."
stability = "stable"
`

const keyProvidersFixture = `[[key_providers]]
kind = "transit"
name = "Vault Transit"
description = "Keeps the key in Vault or OpenBao."
`

const objectStorageFixture = `[[object_storage]]
kind = "s3"
name = "S3"
description = "Keeps objects in S3."
`

func fixtures(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"publishers.toml":           publishersFixture,
		"builtins.toml":             builtinsFixture + keyProvidersFixture + objectStorageFixture,
		"products/deploys.toml":     productFixture,
		"distributions/hosted.toml": distributionFixture,
	}
	writeFixtures(t, dir, files)
	return dir, files
}

func writeFixtures(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func replace(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(s, old, new, 1)
}

func problemText(problems []Problem) string {
	var lines []string
	for _, p := range problems {
		lines = append(lines, p.String())
	}
	return strings.Join(lines, "\n")
}

func TestSchemaRules(t *testing.T) {
	const pub = "publishers.toml"
	const product = "products/deploys.toml"
	const dist = "distributions/hosted.toml"
	const builtins = "builtins.toml"
	const version = "version = \"v1.4.0\""
	const compatibility = "toshi = { min = \"v0.6.0\", max = \"v0.7.0\" }"
	cases := []struct {
		name, file, old, new, want string
	}{
		{"publisher id", pub, `id = "acme"`, `id = "Acme"`, "publishers[0].id"},
		{"publisher name", pub, `name = "Acme"`, `name = ""`, "publishers[0].name"},
		{"publisher url", pub, `https://example.com/`, `http://example.com/`, "publishers[0].url"},
		{"publisher unknown", pub, `name = "Acme"`, "name = \"Acme\"\nextra = true", "publishers[0].extra"},
		{"publisher key case", pub, `name = "Acme"`, `Name = "Acme"`, "publishers[0].Name"},
		{"product id", product, `id = "deploys"`, `id = "Deploys"`, ": id:"},
		{"product filename", product, `id = "deploys"`, `id = "other"`, "must match file name"},
		{"product name empty", product, `name = "Deploys"`, `name = ""`, ": name:"},
		{"product name byte limit", product, `name = "Deploys"`, `name = "` + strings.Repeat("é", 33) + `"`, ": name:"},
		{"description empty", product, `description = "Deploy applications."`, `description = ""`, ": description:"},
		{"description byte limit", product, `description = "Deploy applications."`, `description = "` + strings.Repeat("x", 281) + `"`, ": description:"},
		{"publisher reference", product, `publisher = "acme"`, `publisher = "other"`, ": publisher:"},
		{"module scheme", product, `github.com/acme/deploys`, `https://github.com/acme/deploys`, ": module:"},
		{"module host case", product, `github.com/acme/deploys`, `GitHub.com/acme/deploys`, ": module:"},
		{"module host missing dot", product, `github.com/acme/deploys`, `github/acme/deploys`, ": module:"},
		{"module version", product, `github.com/acme/deploys`, `github.com/acme/deploys@v1.0.0`, ": module:"},
		{"module trailing slash", product, `github.com/acme/deploys`, `github.com/acme/deploys/`, ": module:"},
		{"docs", product, `docs = "https://example.com/docs"`, `docs = "http://example.com/docs"`, ": docs:"},
		{"api", product, `api = "https://example.com/api"`, `api = "http://example.com/api"`, ": surfaces.api:"},
		{"cli", product, `cli = "toshi-deploys"`, `cli = "toshi-1deploys"`, ": surfaces.cli:"},
		{"dashboard", product, `dashboard = "https://example.com/dashboard"`, `dashboard = "http://example.com/dashboard"`, ": surfaces.dashboard:"},
		{"sdk", product, `sdk = "github.com/acme/deploys-go"`, `sdk = ""`, ": surfaces.sdk:"},
		{"unknown top level", product, `id = "deploys"`, "id = \"deploys\"\nextra = true", ": extra:"},
		{"unknown surface", product, `[surfaces]`, "[surfaces]\nextra = true", ": surfaces.extra:"},
		{"unknown version", product, version, version + "\nextra = true", ": versions[0].extra:"},
		{"unknown compatibility", product, compatibility, `toshi = { min = "v0.6.0", extra = true }`, ": versions[0].toshi.extra:"},
		{"missing versions", product, "[[versions]]", "[[not_versions]]", ": versions:"},
		{"bad version", product, version, `version = "1.4.0"`, ": versions[0].version:"},
		{"semver leading zero", product, version, `version = "v1.04.0"`, ": versions[0].version:"},
		{"prerelease leading zero", product, version, `version = "v1.4.0-01"`, ": versions[0].version:"},
		{"stability", product, `stability = "beta"`, `stability = "preview"`, ": versions[0].stability:"},
		{"deprecated missing removed", product, `stability = "beta"`, `stability = "deprecated"`, ": versions[0].removed_in:"},
		{"removed on beta", product, `stability = "beta"`, "stability = \"beta\"\nremoved_in = \"v2.0.0\"", ": versions[0].removed_in:"},
		{"removed not greater", product, `stability = "beta"`, "stability = \"deprecated\"\nremoved_in = \"v1.4.0\"", ": versions[0].removed_in:"},
		{"removed invalid", product, `stability = "beta"`, "stability = \"deprecated\"\nremoved_in = \"2.0.0\"", ": versions[0].removed_in:"},
		{"compatibility missing", product, compatibility, "", ": versions[0].toshi:"},
		{"compatibility min", product, compatibility, `toshi = { max = "v0.7.0" }`, ": versions[0].toshi.min:"},
		{"compatibility max", product, compatibility, `toshi = { min = "v0.6.0", max = "bad" }`, ": versions[0].toshi.max:"},
		{"compatibility order", product, compatibility, `toshi = { min = "v0.8.0", max = "v0.7.0" }`, ": versions[0].toshi.max:"},
		{"product withdrawal reason", product, `[surfaces]`, "[withdrawn]\nsince = 2026-10-28\n[surfaces]", ": withdrawn.reason:"},
		{"product withdrawal missing date", product, `[surfaces]`, "[withdrawn]\nreason = \"reason\"\n[surfaces]", ": withdrawn.since:"},
		{"product withdrawal timestamp", product, `[surfaces]`, "[withdrawn]\nreason = \"reason\"\nsince = 2026-10-28T00:00:00Z\n[surfaces]", ": withdrawn.since:"},
		{"product withdrawal local datetime", product, `[surfaces]`, "[withdrawn]\nreason = \"reason\"\nsince = 2026-10-28T00:00:00\n[surfaces]", ": withdrawn.since:"},
		{"product withdrawal string", product, `[surfaces]`, "[withdrawn]\nreason = \"reason\"\nsince = \"2026-10-28\"\n[surfaces]", ": withdrawn.since:"},
		{"unknown withdrawal", product, `[surfaces]`, "[withdrawn]\nreason = \"reason\"\nsince = 2026-10-28\nextra = true\n[surfaces]", ": withdrawn.extra:"},
		{"version withdrawal reason", product, compatibility, compatibility + "\nwithdrawn = { since = 2026-10-28 }", ": versions[0].withdrawn.reason:"},
		{"version withdrawal date", product, compatibility, compatibility + "\nwithdrawn = { reason = \"reason\" }", ": versions[0].withdrawn.since:"},
		{"version withdrawal unknown", product, compatibility, compatibility + "\nwithdrawn = { reason = \"reason\", since = 2026-10-28, extra = true }", ": versions[0].withdrawn.extra:"},
		{"distribution name", dist, `name = "hosted"`, `name = "Hosted"`, ": distribution.name:"},
		{"distribution unknown", dist, `name = "hosted"`, "name = \"hosted\"\nextra = true", ": distribution.extra:"},
		{"distribution product unknown", dist, `id = "runners"`, "id = \"runners\"\nextra = true", ": products[0].extra:"},
		{"distribution module unknown", dist, `path = "github.com/acme/deploys"`, "path = \"github.com/acme/deploys\"\nextra = true", ": modules[0].extra:"},
		{"distribution product id", dist, `id = "runners"`, `id = "Runners"`, ": product Runners:"},
		{"duplicate product", dist, `id = "deploys"`, `id = "runners"`, "is listed more than once"},
		{"builtin has entry", dist, `id = "deploys"`, "id = \"deploys\"\nbuiltin = true", "builtin must not have a product entry"},
		{"builtin false", dist, `id = "deploys"`, "id = \"deploys\"\nbuiltin = false", "must omit builtin"},
		{"missing product", dist, `id = "deploys"`, `id = "unknown"`, "requires a matching products/unknown.toml entry"},
		{"unknown builtin", dist, `id = "runners"`, `id = "unknown"`, "builtin requires a matching builtins.toml entry"},
		{"builtin id", builtins, `id = "runners"`, `id = "Runners"`, ": builtins[0].id:"},
		{"builtin name", builtins, `name = "Runners"`, `name = ""`, ": builtins[0].name:"},
		{"builtin description", builtins, `description = "Runs jobs."`, `description = ""`, ": builtins[0].description:"},
		{"builtin unknown key", builtins, `name = "Runners"`, "name = \"Runners\"\nextra = true", ": builtins[0].extra:"},
		{"duplicate builtin", builtins, builtinsFixture, builtinsFixture + builtinsFixture, "duplicate builtin"},
		{"builtin is a product", builtins, `id = "runners"`, `id = "deploys"`, "is also a products/deploys.toml entry"},
		{"key provider kind", builtins, `kind = "transit"`, `kind = "Transit"`, ": key_providers[0].kind:"},
		{"key provider name", builtins, `name = "Vault Transit"`, `name = ""`, ": key_providers[0].name:"},
		{"key provider description", builtins, `description = "Keeps the key in Vault or OpenBao."`, `description = ""`, ": key_providers[0].description:"},
		{"key provider unknown key", builtins, `name = "Vault Transit"`, "name = \"Vault Transit\"\nextra = true", ": key_providers[0].extra:"},
		{"duplicate key provider", builtins, keyProvidersFixture, keyProvidersFixture + keyProvidersFixture, "duplicate key provider"},
		{"distribution key provider unknown key", dist, `kind = "transit"`, "kind = \"transit\"\nextra = true", ": key_providers[0].extra:"},
		{"key provider not builtin", dist, "kind = \"transit\"\nbuiltin = true", `kind = "transit"`, "must be builtin = true"},
		{"key provider builtin false", dist, "kind = \"transit\"\nbuiltin = true", "kind = \"transit\"\nbuiltin = false", "key provider transit: must be builtin = true"},
		{"object storage builtin false", dist, "builtin = true\n[[modules]]", "builtin = false\n[[modules]]", "object storage backend s3: must be builtin = true"},
		{"unknown key provider", dist, `kind = "transit"`, `kind = "gcp_kms"`, "builtin requires a matching builtins.toml key_providers entry"},
		{"duplicate distribution key provider", dist, "[[modules]]", "[[key_providers]]\nkind = \"transit\"\nbuiltin = true\n[[modules]]", "key provider transit: is listed more than once"},
		{"object storage kind", builtins, `kind = "s3"`, `kind = "S3"`, ": object_storage[0].kind:"},
		{"object storage name", builtins, `name = "S3"`, `name = ""`, ": object_storage[0].name:"},
		{"object storage description", builtins, `description = "Keeps objects in S3."`, `description = ""`, ": object_storage[0].description:"},
		{"object storage unknown key", builtins, `name = "S3"`, "name = \"S3\"\nextra = true", ": object_storage[0].extra:"},
		{"duplicate object storage", builtins, objectStorageFixture, objectStorageFixture + objectStorageFixture, "duplicate object storage backend"},
		{"distribution object storage unknown key", dist, `kind = "s3"`, "kind = \"s3\"\nextra = true", ": object_storage[0].extra:"},
		{"object storage not builtin", dist, "kind = \"s3\"\nbuiltin = true", `kind = "s3"`, "object storage backend s3: must be builtin = true"},
		{"unknown object storage", dist, `kind = "s3"`, `kind = "gcs"`, "builtin requires a matching builtins.toml object_storage entry"},
		{"duplicate distribution object storage", dist, "[[modules]]", "[[object_storage]]\nkind = \"s3\"\nbuiltin = true\n[[modules]]", "object storage backend s3: is listed more than once"},
		{"builtin stability", builtins, `stability = "stable"`, `stability = "preview"`, ": builtins[0].stability:"},
		{"builtin stability missing", builtins, `stability = "stable"`, "", ": builtins[0].stability:"},
		{"distribution stability missing", dist, "builtin = true\nstability = \"stable\"", "builtin = true", ": product runners: stability must be"},
		{"distribution stability", dist, `stability = "beta"`, `stability = "preview"`, ": product deploys: stability must be"},
		{"builtin stability differs", dist, `stability = "stable"`, `stability = "beta"`, `stability "beta" differs from builtins.toml's "stable"`},
		{"version stability differs", dist, `stability = "beta"`, `stability = "stable"`, `stability "stable" differs from version v1.4.0's "beta"`},
		{"missing module", dist, "[[modules]]\npath = \"github.com/acme/deploys\"\nversion = \"v1.4.0\"\n", "", "requires exactly one module"},
		{"wrong version", dist, version, `version = "v1.5.0"`, "is not listed in the product"},
		{"orphan module", dist, `path = "github.com/acme/deploys"`, `path = "github.com/acme/other"`, "does not belong to a listed catalog product"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, files := fixtures(t)
			files[tc.file] = replace(t, files[tc.file], tc.old, tc.new)
			writeFixtures(t, dir, files)
			_, problems, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := problemText(problems); !strings.Contains(got, tc.want) || !strings.Contains(got, "check: "+tc.file+": ") {
				t.Fatalf("wanted %q in problems for %s; got:\n%s", tc.want, tc.file, got)
			}
		})
	}
}

func TestDuplicateIdentitiesAndModules(t *testing.T) {
	for _, tc := range []struct{ name, contents, want string }{
		{"duplicate id", productFixture, ": id: duplicates"},
		{"duplicate module", strings.Replace(productFixture, `id = "deploys"`, `id = "other"`, 1), ": module: duplicates"},
		{"duplicate version", productFixture + "\n[[versions]]\nversion = \"v1.4.0\"\nstability = \"stable\"\ntoshi = { min = \"v0.6.0\" }\n", ": versions[1].version: duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, files := fixtures(t)
			if tc.name == "duplicate version" {
				files["products/deploys.toml"] = tc.contents
			} else {
				files["products/other.toml"] = tc.contents
			}
			writeFixtures(t, dir, files)
			_, problems, err := Load(dir)
			if err != nil || !strings.Contains(problemText(problems), tc.want) {
				t.Fatalf("wanted %q; problems=%s err=%v", tc.want, problemText(problems), err)
			}
		})
	}
	t.Run("duplicate modules in distribution", func(t *testing.T) {
		dir, files := fixtures(t)
		files["distributions/hosted.toml"] += "\n[[modules]]\npath = \"github.com/acme/deploys\"\nversion = \"v1.4.0\"\n"
		writeFixtures(t, dir, files)
		_, problems, err := Load(dir)
		if err != nil || !strings.Contains(problemText(problems), "found 2") {
			t.Fatalf("problems=%s err=%v", problemText(problems), err)
		}
	})
}

func TestWithdrawalTransitions(t *testing.T) {
	dir, files := fixtures(t)
	check := func(want string) {
		t.Helper()
		writeFixtures(t, dir, files)
		c, problems, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := problemText(problems)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Fatalf("wanted %q, got:\n%s", want, got)
		}
		module, version, hosted := c.Hosted("deploys")
		if want == "" && strings.Contains(files["distributions/hosted.toml"], `id = "deploys"`) {
			if !hosted || module != "github.com/acme/deploys" || version != "v1.4.0" {
				t.Fatalf("hosted: %q %q %v", module, version, hosted)
			}
		} else if hosted {
			t.Fatalf("withdrawn or absent product was reported hosted")
		}
	}
	check("")
	files["products/deploys.toml"] = replace(t, productFixture, "[surfaces]", "[withdrawn]\nreason = \"reason\"\nsince = 2026-10-28\n[surfaces]")
	check("product is withdrawn")
	files["products/deploys.toml"] = productFixture + "withdrawn = { reason = \"reason\", since = 2026-10-28 }\n"
	check("module version \"v1.4.0\" is withdrawn")
	files["distributions/hosted.toml"] = "[distribution]\nname = \"hosted\"\n[[products]]\nid = \"runners\"\nbuiltin = true\nstability = \"stable\"\n"
	check("")
	files["products/deploys.toml"] = replace(t, productFixture, "[surfaces]", "[withdrawn]\nreason = \"reason\"\nsince = 2026-10-28\n[surfaces]")
	check("")
}

func TestDistributionProductIsolation(t *testing.T) {
	dir, files := fixtures(t)
	files["products/alpha.toml"] = strings.ReplaceAll(strings.ReplaceAll(productFixture, "deploys", "alpha"), "Deploys", "Alpha") +
		"withdrawn = { reason = \"reason\", since = 2026-10-28 }\n"
	files["distributions/hosted.toml"] += "\n[[products]]\nid = \"alpha\"\nstability = \"beta\"\n[[modules]]\npath = \"github.com/acme/alpha\"\nversion = \"v1.4.0\"\n"
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 1 || problems[0].Field != "product alpha" || !strings.Contains(problems[0].Message, "withdrawn") {
		t.Fatalf("problems=%s err=%v", problemText(problems), err)
	}
	if _, _, ok := c.Hosted("deploys"); !ok {
		t.Fatal("the other product must remain hosted")
	}
	if _, _, ok := c.Hosted("alpha"); ok {
		t.Fatal("the withdrawn version must not be hosted")
	}
}

func TestHostedDoesNotCombineDistributions(t *testing.T) {
	dir, files := fixtures(t)
	files["distributions/hosted.toml"] = "[distribution]\nname = \"hosted\"\n[[products]]\nid = \"deploys\"\nstability = \"beta\"\n"
	files["distributions/other.toml"] = "[distribution]\nname = \"hosted\"\n[[modules]]\npath = \"github.com/acme/deploys\"\nversion = \"v1.4.0\"\n"
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 2 {
		t.Fatalf("problems=%s err=%v", problemText(problems), err)
	}
	if _, _, ok := c.Hosted("deploys"); ok {
		t.Fatal("lookup combined a product and module from different distributions")
	}
}

func TestHostedUsesCanonicalManifest(t *testing.T) {
	dir, files := fixtures(t)
	files["distributions/other.toml"] = "[distribution]\nname = \"hosted\"\n"
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems=%s err=%v", problemText(problems), err)
	}
	if module, version, ok := c.Hosted("deploys"); !ok || module != "github.com/acme/deploys" || version != "v1.4.0" {
		t.Fatalf("hosted = %q %q %v", module, version, ok)
	}
}

func TestVersionWithdrawalIsolation(t *testing.T) {
	dir, files := fixtures(t)
	other := "\n[[versions]]\nversion = \"v1.5.0\"\nstability = \"beta\"\ntoshi = { min = \"v0.6.0\" }\n"
	withdrawal := "withdrawn = { reason = \"reason\", since = 2026-10-28 }\n"
	files["products/deploys.toml"] = productFixture + other + withdrawal
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("unrelated version withdrawal: problems=%s err=%v", problemText(problems), err)
	}
	if latest := c.Products[0].Latest(); latest == nil || latest.Version != "v1.4.0" {
		t.Fatalf("latest = %v", latest)
	}
	if _, version, ok := c.Hosted("deploys"); !ok || version != "v1.4.0" {
		t.Fatalf("hosted = %q, %v", version, ok)
	}
	files["distributions/hosted.toml"] = strings.Replace(files["distributions/hosted.toml"], `version = "v1.4.0"`, `version = "v1.5.0"`, 1)
	writeFixtures(t, dir, files)
	_, problems, err = Load(dir)
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0].Message, `version "v1.5.0" is withdrawn`) {
		t.Fatalf("withdrawn selected version: problems=%s err=%v", problemText(problems), err)
	}
	files["products/deploys.toml"] = productFixture + withdrawal + other
	writeFixtures(t, dir, files)
	c, problems, err = Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("different version withdrawn: problems=%s err=%v", problemText(problems), err)
	}
	if _, version, ok := c.Hosted("deploys"); !ok || version != "v1.5.0" {
		t.Fatalf("hosted = %q, %v", version, ok)
	}
}

func TestOptionalAndSemanticVersions(t *testing.T) {
	dir, files := fixtures(t)
	files["publishers.toml"] = strings.Replace(publishersFixture, `name = "Acme"`, `name = " "`, 1)
	files["products/deploys.toml"] = strings.Replace(productFixture,
		"[surfaces]\napi = \"https://example.com/api\"\ncli = \"toshi-deploys\"\ndashboard = \"https://example.com/dashboard\"\nsdk = \"github.com/acme/deploys-go\"\n", "", 1)
	files["products/deploys.toml"] = strings.Replace(files["products/deploys.toml"], `name = "Deploys"`, `name = " "`, 1)
	files["products/deploys.toml"] = strings.Replace(files["products/deploys.toml"], `description = "Deploy applications."`, `description = "`+strings.Repeat("x", 280)+`"`, 1)
	files["products/deploys.toml"] = strings.Replace(files["products/deploys.toml"], `stability = "beta"`, "stability = \"deprecated\"\nremoved_in = \"v2.0.0\"", 1)
	files["products/deploys.toml"] = strings.Replace(files["products/deploys.toml"], `toshi = { min = "v0.6.0", max = "v0.7.0" }`, `toshi = { min = "v0.6.0" }`, 1)
	files["distributions/hosted.toml"] = strings.Replace(distributionFixture, `stability = "beta"`, `stability = "deprecated"`, 1)
	files["products/deploys.toml"] += "\n[[versions]]\nversion = \"v1.5.0-rc.1+build\"\nstability = \"alpha\"\ntoshi = { min = \"v0.7.0\", max = \"v0.7.0\" }\n"
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems=%s err=%v", problemText(problems), err)
	}
	if latest := c.Products[0].Latest(); latest == nil || latest.Version != "v1.5.0-rc.1+build" {
		t.Fatalf("latest = %v", latest)
	}
}

func TestCollectAndIsolateProblems(t *testing.T) {
	dir, files := fixtures(t)
	files["products/deploys.toml"] = strings.Replace(productFixture, `name = "Deploys"`, "name = 123\ndescription_extra = true", 1)
	files["products/deploys.toml"] = strings.Replace(files["products/deploys.toml"], `docs = "https://example.com/docs"`, `docs = false`, 1)
	files["products/other.toml"] = "id = \"other\"\nname = [\n"
	writeFixtures(t, dir, files)
	_, problems, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := problemText(problems)
	for _, want := range []string{": name: must be a string", ": docs: must be a string", ": description_extra: unknown field", "products/other.toml: toml:"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestInvalidDateIsReportedOnce(t *testing.T) {
	dir, files := fixtures(t)
	files["products/deploys.toml"] = replace(t, productFixture, "[surfaces]", "[withdrawn]\nreason = \"reason\"\nsince = \"2026-10-28\"\n[surfaces]")
	writeFixtures(t, dir, files)
	_, problems, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range problems {
		if p.File == "products/deploys.toml" && p.Field == "withdrawn.since" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one date problem, got %d: %s", count, problemText(problems))
	}
}

func TestLatestAndSemanticPrecedence(t *testing.T) {
	versions := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0"}
	for i := 1; i < len(versions); i++ {
		if compareVersions(versions[i-1], versions[i]) >= 0 {
			t.Errorf("%s should precede %s", versions[i-1], versions[i])
		}
	}
	if compareVersions("v1.99999999999999999999.0", "v1.100000000000000000000.0") >= 0 || compareVersions("v1.0.0+one", "v1.0.0+two") != 0 {
		t.Fatal("incorrect numeric or build precedence")
	}
	p := Product{Versions: []Version{{Version: "v1.0.0"}, {Version: "v2.0.0", Withdrawal: &Withdrawal{}}, {Version: "v1.1.0-rc.1"}}}
	if v := p.Latest(); v == nil || v.Version != "v1.1.0-rc.1" {
		t.Fatalf("latest = %v", v)
	}
	p.Versions = []Version{{Version: "v2.0.0", Withdrawal: &Withdrawal{}}}
	if p.Latest() != nil {
		t.Fatal("withdrawn version returned as latest")
	}
}

func TestLocalDateAndOrdering(t *testing.T) {
	dir, files := fixtures(t)
	files["products/deploys.toml"] += "withdrawn = { reason = \"reason\", since = 2026-10-28 }\n"
	files["distributions/hosted.toml"] = "[distribution]\nname = \"hosted\"\n"
	files["products/alpha.toml"] = strings.ReplaceAll(strings.ReplaceAll(productFixture, "deploys", "alpha"), "Deploys", "Alpha")
	files["publishers.toml"] += "\n[[publishers]]\nid = \"alpha\"\nname = \"Alpha\"\nurl = \"https://example.com\"\n"
	writeFixtures(t, dir, files)
	c, problems, err := Load(dir)
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems=%s err=%v", problemText(problems), err)
	}
	if c.Publishers[0].ID != "acme" || c.Products[0].ID != "alpha" || c.Products[1].Versions[0].Withdrawal.Date() != "2026-10-28" {
		t.Fatalf("unexpected order or date: %+v", c)
	}
}
