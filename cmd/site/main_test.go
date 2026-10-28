// Site tests cover rendered catalog data, escaping and validation failures.
package main

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toshicloud/toshi-catalog/internal/catalog"
)

const testPublishers = `[[publishers]]
id = "acme"
name = "Acme & Co"
url = "https://example.com/"
`

const testProduct = `id = "deploys"
name = "Deploys"
description = "Deploy <script>alert(1)</script> safely."
publisher = "acme"
module = "github.com/acme/deploys"
docs = "https://example.com/docs?a=1&b=2"

[surfaces]
api = "https://example.com/api"
cli = "toshi-deploys"
dashboard = "https://example.com/dashboard"
sdk = "github.com/acme/deploys-go"

[[versions]]
version = "v1.4.0"
stability = "deprecated"
removed_in = "v2.0.0"
toshi = { min = "v0.6.0", max = "v0.7.0" }

[[versions]]
version = "v1.8.0"
stability = "stable"
toshi = { min = "v0.8.0" }

[[versions]]
version = "v2.0.0"
stability = "beta"
toshi = { min = "v0.8.0" }
withdrawn = { reason = "Unsafe <script> release", since = 2026-10-20 }
`

const testArchive = `id = "archive"
name = "Archive"
description = "Archived product."
publisher = "acme"
module = "github.com/acme/archive"
docs = "https://example.com/archive"
[[versions]]
version = "v1.0.0"
stability = "stable"
toshi = { min = "v0.6.0" }
withdrawn = { reason = "Unavailable", since = 2026-10-21 }
`

const testRetired = `id = "retired"
name = "Retired"
description = "Retired product."
publisher = "acme"
module = "github.com/acme/retired"
docs = "https://example.com/retired"
withdrawn = { reason = "No longer available", since = 2026-10-22 }
[[versions]]
version = "v1.0.0"
stability = "stable"
toshi = { min = "v0.6.0" }
`

const testExtra = `id = "extras"
name = "Extras"
description = "Another distribution."
publisher = "acme"
module = "github.com/acme/extras"
docs = "https://example.com/extras"
[[versions]]
version = "v1.0.0"
stability = "alpha"
toshi = { min = "v0.6.0" }
`

const testHosted = `
# Hosted manifest <script> must remain text.
[distribution]
name = "hosted"
[[products]]
id = "runners"
builtin = true
[[products]]
id = "deploys"
[[products]]
id = "previews"
builtin = true
[[modules]]
path = "github.com/acme/deploys"
version = "v1.4.0"
`

const testBuiltins = `[[builtins]]
id = "runners"
name = "Runners"
description = "Runs jobs."
[[builtins]]
id = "previews"
name = "Previews"
description = "Serves previews."
[[builtins]]
id = "other_builtin"
name = "Other"
description = "Built into another distribution."
`

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "publishers.toml", testPublishers)
	writeFixture(t, dir, "builtins.toml", testBuiltins)
	writeFixture(t, dir, "products/deploys.toml", testProduct)
	writeFixture(t, dir, "products/archive.toml", testArchive)
	writeFixture(t, dir, "products/retired.toml", testRetired)
	writeFixture(t, dir, "products/extras.toml", testExtra)
	writeFixture(t, dir, "distributions/hosted.toml", strings.ReplaceAll(testHosted, "\n", "\r\n"))
	writeFixture(t, dir, "distributions/other.toml", "[distribution]\nname = \"other\"\n[[products]]\nid = \"other_builtin\"\nbuiltin = true\n[[products]]\nid = \"extras\"\n[[modules]]\npath = \"github.com/acme/extras\"\nversion = \"v1.0.0\"\n")
	return dir
}

func readOutput(t *testing.T, out, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireContains(t *testing.T, text string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Errorf("output does not contain %q", value)
		}
	}
}

func productCard(t *testing.T, index, id string) string {
	t.Helper()
	start := strings.Index(index, "products/"+id+".html")
	if start < 0 {
		t.Fatalf("missing product %s in index", id)
	}
	end := strings.Index(index[start:], "</li>")
	if end < 0 {
		t.Fatalf("missing closing element for product %s", id)
	}
	return index[start : start+end]
}

func TestRunBuildsSite(t *testing.T) {
	dir, out := fixture(t), t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-out", out}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if want := "index.html\nproducts/archive.html\nproducts/deploys.html\nproducts/extras.html\nproducts/retired.html\nhosted.html\nstyle.css\n"; stdout.String() != want {
		t.Errorf("files written = %q, want %q", stdout.String(), want)
	}
	index := readOutput(t, out, "index.html")
	product := readOutput(t, out, "products/deploys.html")
	hosted := readOutput(t, out, "hosted.html")
	readOutput(t, out, "style.css")
	requireContains(t, index, "Acme &amp; Co", "v1.8.0", "stable", "Hosted", "hosted.html", "&lt;script&gt;alert(1)&lt;/script&gt;")
	if !(strings.Index(index, "products/archive.html") < strings.Index(index, "products/deploys.html") && strings.Index(index, "products/deploys.html") < strings.Index(index, "products/extras.html") && strings.Index(index, "products/extras.html") < strings.Index(index, "products/retired.html")) {
		t.Error("products are not in ID order")
	}
	requireContains(t, productCard(t, index, "archive"), ">Withdrawn</span>")
	requireContains(t, productCard(t, index, "retired"), ">Withdrawn</span>")
	requireContains(t, productCard(t, index, "deploys"), ">Hosted</span>")
	extraCard := productCard(t, index, "extras")
	if strings.Contains(extraCard, ">Hosted</span>") {
		t.Error("a product from another distribution is marked hosted")
	}
	requireContains(t, product, "github.com/acme/deploys", "go get github.com/acme/deploys@v1.8.0", "Copy module path", "Copy go get", "Copy URL", "v1.4.0", "v2.0.0", "v0.6.0 to v0.7.0", "Unsafe &lt;script&gt; release (2026-10-20)", "https://example.com/api", "https://example.com/dashboard", "toshi-deploys", "github.com/acme/deploys-go", "https://example.com/docs?a=1&amp;b=2", "Hosted: ships version v1.4.0")
	requireContains(t, hosted, "Built into Toshi", "<strong>Previews</strong> (<code>previews</code>): Serves previews.", "<strong>Runners</strong> (<code>runners</code>): Runs jobs.", "github.com/acme/deploys", "v1.4.0", "Copy module path", "Copy version", "data-copy-target=\"hosted-manifest\"")
	if strings.Index(hosted, "<code>previews</code>") >= strings.Index(hosted, "<code>runners</code>") || strings.Contains(hosted, "other_builtin") || strings.Contains(hosted, "github.com/acme/extras") {
		t.Error("built-ins are not ordered or came from another distribution")
	}
	const manifestTag = "<pre><code id=\"hosted-manifest\">"
	requireContains(t, hosted, manifestTag+"&#13;\n# Hosted manifest")
	start := strings.Index(hosted, manifestTag)
	if start < 0 {
		t.Fatal("manifest element missing")
	}
	encoded, _, ok := strings.Cut(hosted[start+len(manifestTag):], "</code></pre>")
	if !ok || strings.Contains(encoded, "\r") || html.UnescapeString(encoded) != strings.ReplaceAll(testHosted, "\n", "\r\n") {
		t.Error("manifest does not preserve its original text and line endings")
	}
	for name, page := range map[string]string{"index": index, "product": product, "hosted": hosted} {
		if strings.Contains(page, "<script>") {
			t.Errorf("%s contains unescaped entry text", name)
		}
	}
	archive := readOutput(t, out, "products/archive.html")
	extra := readOutput(t, out, "products/extras.html")
	retired := readOutput(t, out, "products/retired.html")
	requireContains(t, archive, "Unavailable (2026-10-21)", "Not shipped by the hosted deployment")
	requireContains(t, extra, "go get github.com/acme/extras@v1.0.0", "Not shipped by the hosted deployment")
	requireContains(t, retired, "Withdrawn:", "No longer available (2026-10-22)")
	if strings.Contains(archive, "go get ") || strings.Contains(retired, "go get ") {
		t.Error("withdrawn product offers an install command")
	}
}

func TestRunRejectsInvalidCatalog(t *testing.T) {
	dir, out := fixture(t), t.TempDir()
	writeFixture(t, dir, "publishers.toml", "[[publishers]]\nid = \"bad id\"\nname = \"\"\nurl = \"http://example.com\"\n")
	writeFixture(t, out, "products/old.html", generatedHeader+"old page")
	_, problems, err := catalog.Load(dir)
	if err != nil || len(problems) < 2 {
		t.Fatalf("fixture problems=%v err=%v", problems, err)
	}
	var want strings.Builder
	for _, problem := range problems {
		fmt.Fprintln(&want, problem.String())
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-out", out}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.String() != want.String() {
		t.Fatalf("code=%d stdout=%q stderr=%q, want stderr %q", code, stdout.String(), stderr.String(), want.String())
	}
	if got := readOutput(t, out, "products/old.html"); got != generatedHeader+"old page" {
		t.Errorf("old page changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); !os.IsNotExist(err) {
		t.Errorf("invalid catalog wrote index.html: %v", err)
	}
}

func TestRunRebuildsDeterministically(t *testing.T) {
	dir, out := fixture(t), t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"-dir", dir, "-out", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("first build: %q", stderr.String())
	}
	before := readOutput(t, out, "products/deploys.html")
	writeFixture(t, out, "products/notes.html", "not generated")
	if err := os.Remove(filepath.Join(dir, "products", "archive.toml")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("second build: %q", stderr.String())
	}
	if after := readOutput(t, out, "products/deploys.html"); before != after {
		t.Error("unchanged product page changed between builds")
	}
	if _, err := os.Stat(filepath.Join(out, "products", "archive.html")); !os.IsNotExist(err) {
		t.Errorf("stale page remains: %v", err)
	}
	if got := readOutput(t, out, "products/notes.html"); got != "not generated" {
		t.Errorf("unrelated page changed: %q", got)
	}
}

func TestRunFlagsAndHostedManifest(t *testing.T) {
	dir, out := fixture(t), t.TempDir()
	for _, args := range [][]string{{"-dir", dir}, {"-out", out, "extra"}, {"-unknown"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	if err := os.Remove(filepath.Join(dir, "distributions", "hosted.toml")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-dir", dir, "-out", out}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "distributions/hosted.toml") {
		t.Errorf("missing hosted manifest: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
