# Entries

One product per file. The signed index is built from this folder, so an entry
exists once its file has been signed into an index.

## The file

One entry object per file, in the shape the Toshi catalog documentation's entry
table describes: `id`, `name`, `publisher`, `version`, `description`,
`surfaces` (`api`, `cli`, `dashboard`, `sdk`), `access`, `runs` (`where`,
`app`), `docs`, `state` (`state`, `reason`, `successor`), `releases` and
`withdrawn`. A release carries `version`, `api`, `host`, `artifacts` and its own
`state`. A withdrawn release carries `version`, `digests`, `reason`,
`effective`, `successor` and `state`.

Use the keys and the types the documentation lists. A key the table does not
list is refused, and so is a value of the wrong type.

## Naming

`entries/<publisher>/<id>.json`: the folder is the entry's publisher, and the
file name is its id plus `.json`. The `publisher` and `id` inside the file must
match, and the publisher must be one `catalog.toml` lists.

## Proposing an entry

A publisher opens a pull request that adds or changes a file under its own
folder, and a maintainer reviews it. Until it is signed, the check fails on that
pull request: the committed index no longer lists what this folder holds.

Once the entry is accepted, a maintainer runs `scripts/sign` on the pull
request's branch and pushes the signed index and its generation. That builds the
index from this folder and signs it, the check passes, and the pull request is
merged signed. Deployments read the entry once the signed index is on main.
