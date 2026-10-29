# toshi-catalog

The directory of Toshi products, and the manifest of the hosted Toshi
deployment. A deployment never fetches anything from here: products are Go
modules, and a deployment's builder compiles them into the deployment it
builds. This repository records which products exist, who publishes them, and
which of them the hosted deployment ships.

The directory is published as a site at
<https://toshicloud.github.io/toshi-catalog/>.

## Files

| Path | What it is |
| --- | --- |
| `publishers.toml` | One `[[publishers]]` table per publisher, with its name and URL. |
| `builtins.toml` | One `[[builtins]]` table per product that is part of Toshi itself, with its name, description and stability, one `[[key_providers]]` table per key provider engine that is, and one `[[object_storage]]` table per object storage backend that is, each with its name and description. |
| `products/` | One TOML file per product, `<id>.toml`, named by the product's `id`. |
| `distributions/` | One TOML file per deployment manifest. `hosted.toml` is the hosted deployment's. |
| `internal/catalog` | Loads and validates publishers, products and distributions. |
| `cmd/check` | Runs the validation over the directory and exits non-zero with a line per problem. |
| `cmd/site` | Builds the published site from the directory. |

## Adding a product

1. If the publisher is new, add it to `publishers.toml`.
2. Add `products/<id>.toml`. The file name must match the `id` inside it, and
   the file's `publisher` must name a publisher in `publishers.toml`.
3. Open a pull request. CI checks the change, and the maintainers review it.

`products/README.md` describes every field.

## Adding it to the hosted deployment

Open a pull request that adds the product and its module to
`distributions/hosted.toml`: a `[[products]]` table naming the product and the
stability of the version that ships, and a `[[modules]]` table giving the
product's module path and that version. Each Toshi release builds from this file at a commit the release names, so
the release that follows the pull request ships it.

A `[[products]]` table with `builtin = true` names a part of Toshi itself: its
`id` must be one `builtins.toml` lists, its `stability` the one listed there,
and it needs no product file. A Toshi build refuses a manifest whose stability
for a builtin differs from the stability the product declares in that build.

A `[[key_providers]]` table names a key provider engine the deployment's
`toshi-key-provider` links, by its `kind`, with `builtin = true`: every key
provider is part of Toshi itself, and its kind must be one `builtins.toml`
lists.

An `[[object_storage]]` table names an object storage backend the deployment's
`toshi-control` links, by its `kind`, with `builtin = true`, in the same way:
every object storage backend is part of Toshi itself, and its kind must be one
`builtins.toml` lists.

## Withdrawing a product or a version

Mark the product, or the version, withdrawn with a `reason` and a `since` date,
and remove it from `distributions/hosted.toml` in the same pull request. The
check refuses a manifest that lists a withdrawn product or a withdrawn version,
so the two changes go together. The release that follows drops it.

## Self-hosted deployments

Copy `distributions/hosted.toml` as the starting point for your own manifest,
or write one: a `[distribution]` table, the `[[products]]` it ships with
each one's stability, and a
`[[modules]]` table per product giving its module path and version.

## What the check enforces

- `publishers.toml`: an id that starts with a lowercase letter and holds only
  lowercase letters, digits and underscores; a unique id; a name; a URL.
- `products/`: the file name matches the `id`; a publisher that exists; a name,
  a description, a module path and at least one version; semantic versions and
  compatibility bounds; `removed_in` only on a deprecated version, and higher
  than it.
- `builtins.toml`: a unique id in the same form as a product's, a name of 1–64
  bytes, a description of 1–280 bytes, a stability (`alpha`, `beta`, `stable`
  or `deprecated`), and no products/ entry with the same id; for each key
  provider and each object storage backend, a unique kind in the same form, a
  name and a description.
- `distributions/`: every builtin, key provider and object storage backend is
  one `builtins.toml` lists, every key provider and object storage backend is
  listed once with `builtin = true`, every other
  product and every module resolves to a product entry, a module version is one
  the product lists, and nothing listed is withdrawn. Every listed product has
  a stability: a builtin's is the one `builtins.toml` gives, a catalog
  product's that of the version its module ships.
- Withdrawal: a product or a version marked withdrawn carries a reason and a
  date, and no distribution lists it.

## Maintainers

The `catalog-maintainers` team maintains the directory. `.github/CODEOWNERS`
names it for the whole repository, so pull requests ask it for review. The rules
on `main` require no approval: the file routes review requests and gates
nothing. A publisher's product files get their own line when its first product
lands.

## The site

`cmd/site -out <directory>` renders the directory into a static site: one page
per product, an index, and the hosted deployment's manifest, with copy buttons
for module paths, versions and URLs. The workflow builds it on every pull request, and on
a push to `main` publishes it to GitHub Pages.
