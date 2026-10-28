# toshi-catalog

The catalog of products Toshi lists by default. It is signed, so a Toshi
deployment can fetch it and check that the list it reads is the one the key
holder published.

## Files

| File | What it is |
| --- | --- |
| `catalog.toml` | The `[[catalog.sources]]` entry a deployment copies into its own configuration. |
| `index.json` | The catalog itself, served at the URL in `catalog.toml`. |
| `index.json.sig` | The signature over `index.json`. |
| `generations/` | Each signed index as it was published: one directory per generation. Never edited. |
| `cmd/check` | The checker that validates the catalog. |
| `scripts/sign` | The signing tool the key holder runs. |

## Listing the catalog in a deployment

Copy the `[[catalog.sources]]` entry from `catalog.toml` into the deployment's
configuration. The deployment then fetches `index.json` from that URL and accepts
it only if it verifies against one of the keys in the entry.

## Signing and publishing

The key holder runs:

```
scripts/sign <key.pem> [expires-in]
```

It needs the Toshi CLI, Go and `openssl`. It signs a new index and writes its
generation. Every change to the catalog is a higher sequence: an entry, a
release, a state change or a block. A published generation is never edited;
the check refuses a pull request that changes, deletes or renames one.

A release or an entry that must stop being installed is blocked the same way,
by signing a higher sequence with its state set to `blocked`. A block carries
a reason, and for a release taken back, when it took effect. Its successor may
be absent: something can be blocked without naming what replaces it.

Re-sign before the index expires.

## What installations do

- **listed**: installs as usual. An entry with no state is listed.
- **deprecated**: no new installations, unless the workspace names the release.
  Installations that exist keep working. The API and the CLI show the reason
  and the successor.
- **blocked**: suspended, as turning the product off does, and
  `product.blocked` is recorded for each installation, with the source and the
  reason. A suspended installation does not turn on again when a later index
  lifts the block; each workspace turns the product on again.
- **no longer listed**: an entry the source stops listing is treated as
  deprecated, with the reason `no longer listed by <source>`, until an index
  lists it again. Its installations keep working.

## Expiry

An index carries an expiry. A deployment accepts a new index only while it has
not expired, and takes an index it already accepted again as it is, even once
it has expired.

The scheduled check reads the served index once a week, so an index left to
expire is noticed. A source is stale once its accepted index expired more than
7 days ago. Its products are still listed, marked stale, and installing one is
refused; installations made before keep running, and a workspace may turn one
off and on again. Serving a new index makes the source fresh again.

## The check

The check refuses an index whose signature does not verify against a key listed
in `catalog.toml`, an index that has reached its expiry, and, on a pull request,
a sequence lower than the base branch's or the same sequence with different
bytes. It also refuses a pull request that changes, deletes or renames a file
under `generations/`.

It runs on every pull request and every push to `main` that touches the catalog,
`catalog.toml`, the generations, the checker, the Go module files,
`.gitattributes` or the workflow. Those jobs read the repository and nothing
else.

Once a week, and on demand, a second job checks the index as served, with the
same expiry rule, and fails when it cannot reach it or when the index it reads
is invalid.

## Move to a new key

A source's `keys` is a list, and an index signed by any key in it is accepted.

1. Make the new key, and publish its `keys` line to the deployments.
2. Each deployment adds it to `keys` beside the old one.
3. Sign the next index with the new key, and serve it.
4. Once the deployments have read it, each removes the old key.

A deployment that has not added the new key refuses the new index and keeps the
last one it accepted, until its index expires.

## The key holder

- The private key never enters this repository, GitHub Actions, or any secret
  store the workflow reads. Signing happens on the key holder's own machine,
  with `scripts/sign`.
- Pull-request jobs have read-only permissions: they can check a change but
  never write to the repository.
- The actions the workflow uses are pinned to full commit shas.
- The scheduled job runs without the key. When it fails it opens an issue
  titled `Catalog check failing`, or comments on the open one, with the run
  URL, so a missed signature or an unreachable index is noticed whether or not
  the key holder is watching. Nothing closes it automatically: close it once
  the check passes.

## Generations

`generations/` holds each signed index as it was published, one directory per
generation, so a sequence that has been served can be read back exactly as it
was signed.

Nothing in it is ever edited. A published generation is the record of what the
key holder signed, and a change to it would mean the same sequence having two
different bytes, which deployments refuse. To change the catalog, sign a
higher sequence, which adds a new generation.

## Until the key is set

`catalog.toml` carries the placeholder key
`ed25519:REPLACE_WITH_THE_CATALOG_PUBLIC_KEY`. Until it is replaced with the real
public key, the check fails on that placeholder by design.
