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
`distributions/hosted.toml`: a `[[products]]` table naming the product, and a
`[[modules]]` table giving the product's module path and the version that ships
it. The release that follows the pull request ships it.

A `[[products]]` table with `builtin = true` names a part of Toshi itself and
needs no product file.

## Withdrawing a product or a version

Mark the product, or the version, withdrawn with a `reason` and a `since` date,
and remove it from `distributions/hosted.toml` in the same pull request. The
check refuses a manifest that lists a withdrawn product or a withdrawn version,
so the two changes go together. The release that follows drops it.

## Self-hosted deployments

Copy `distributions/hosted.toml` as the starting point for your own manifest,
or write one: a `[distribution]` table, the `[[products]]` it ships, and a
`[[modules]]` table per product giving its module path and version.

## What the check enforces

- `publishers.toml`: an id that starts with a lowercase letter and holds only
  lowercase letters, digits and underscores; a unique id; a name; a URL.
- `products/`: the file name matches the `id`; a publisher that exists; a name,
  a description, a module path and at least one version; semantic versions and
  compatibility bounds; `removed_in` only on a deprecated version, and higher
  than it.
- `distributions/`: every product and every module resolves to a product entry,
  a module version is one the product lists, and nothing listed is withdrawn.
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
