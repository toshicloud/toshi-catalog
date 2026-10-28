# toshi-catalog

The catalog of products Toshi lists by default. It is signed, so a Toshi
deployment can fetch it and check that the list it reads is the one the key
holder published.

## Files

| File | What it is |
| --- | --- |
| `catalog.toml` | The `[[catalog.sources]]` entry a deployment copies into its own configuration. |
| `index.json` | The catalog itself. |
| `index.json.sig` | The signature over `index.json`. |
| `cmd/check` | The checker that validates the catalog. |
| `scripts/sign` | The signing tool the key holder runs. |

## Listing the catalog in a deployment

Copy the `[[catalog.sources]]` entry from `catalog.toml` into the deployment's
configuration. The deployment then fetches `index.json` from that URL and accepts
it only if it verifies against one of the keys in the entry.

## The check

The check refuses an index whose signature does not verify against a key listed
in `catalog.toml`, an index that has reached its expiry, and, on a pull request,
a sequence lower than the base branch's or the same sequence with different
bytes.

It runs on every pull request and every push to `main` that touches the catalog,
`catalog.toml`, the checker, the Go module files or the workflow, and once a week
so that an index left to expire is noticed.

## Signing

The key holder runs:

```
scripts/sign <key.pem> [expires-in]
```

It needs the Toshi CLI and `openssl`. The private key stays with the key holder:
it never enters this repository and is never given to CI.

Re-sign before the index expires.

To move to a new key, list both keys in `catalog.toml`, sign the index with the
new one, and remove the old key once deployments have the line with both keys.

## Until the key is set

`catalog.toml` carries the placeholder key
`ed25519:REPLACE_WITH_THE_CATALOG_PUBLIC_KEY`. Until it is replaced with the real
public key, the check fails on that placeholder by design.
