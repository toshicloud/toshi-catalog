# Products

Each product has one TOML file in this flat folder: `products/<id>.toml`.
The `id` inside the file must match its file name. IDs start with a lowercase
letter and contain only lowercase letters, digits and underscores. A new
publisher adds itself to `publishers.toml` in its first pull request.

For example, `products/deploys.toml` can contain:

```toml
id = "deploys"
name = "Deploys"
description = "Deploy applications."
publisher = "acme"
module = "github.com/acme/deploys"
docs = "https://example.com/deploys"

[surfaces]
api = "https://example.com/deploys/api"
cli = "toshi-deploys"
dashboard = "https://example.com/deploys/dashboard"
sdk = "github.com/acme/deploys-go"

[[versions]]
version = "v1.4.0"
stability = "beta"
toshi = { min = "v0.6.0", max = "v0.7.0" }

[[versions]]
version = "v1.3.0"
stability = "deprecated"
removed_in = "v2.0.0"
toshi = { min = "v0.6.0" }
withdrawn = { reason = "A defect affects this version.", since = 2026-10-28 }
```

The publisher must be listed in `publishers.toml`. Names are 1–64 bytes and
descriptions are 1–280 bytes. The module is an unversioned Go module path;
`docs`, `api` and `dashboard` are HTTPS URLs. Surface fields are optional.
At least one version is required. Versions and compatibility bounds use
semantic versions beginning with `v`. Stability is `alpha`, `beta`, `stable`
or `deprecated`; only a deprecated version has `removed_in`, which must be
greater than that version. The maximum Toshi version is optional and cannot
precede the minimum. A product or version may omit its `withdrawn` table;
when present it requires a reason and a local date.

To withdraw the whole product, add a top-level table:

```toml
[withdrawn]
reason = "This product is no longer available."
since = 2026-10-28
```

Remove a withdrawn product or version from `distributions/hosted.toml` in the
same pull request: the check refuses a manifest that lists it.
