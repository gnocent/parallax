# Contributing to Parallax

*[Français](CONTRIBUTING.md) · English*

## Before writing code

Read [`CLAUDE.md`](CLAUDE.md): it is the project's technical reference —
stack, conventions, business glossary, capacity calculation order,
repository layout, and the state of every feature. It was written to guide
an AI assistant working on the code, but it is just as useful to a person
discovering the project.

To understand the data schema and its invariants in detail,
[`docs/modele-donnees.en.md`](docs/modele-donnees.en.md) (data model) is the
technical reference. The [user guide](docs/guide-utilisateur.en.md) explains
what the application does; this document explains how it does it.

## Environment

- Go 1.22 or later
- Python 3.8 or later, only to replay the executable specification (if
  absent, `make check` falls back to the already-committed vectors)
- No external service: SQLite is embedded, no database to install

## Checking your work

```sh
make check
```

Replays the executable specification, `go mod tidy`, the build, `go vet` and
the full test suite. It is the only command you need to know, and the
project expects it to stay green on every commit.

## The executable specification

`spec/capacity_reference.py` is the reference implementation of the capacity
engine. It produces `spec/vectors.json`, which the Go tests consume as is.
**The Python reference is authoritative**: any divergence is a bug on the Go
side. To change a calculation rule, edit the Python first, regenerate the
vectors, then bring the Go code into line — never the other way around, and
never by hand-adjusting a vector to make a test pass.

`spec/validate_schema.py` runs the schema under SQLite against a
representative dataset and checks the trickiest queries (variable
inheritance, fleet state as of a past date, invisibility of a hypothetical
server outside its scenario, detection of overlap between rules).

## Conventions to follow

- Identifiers, comments and error messages in French, matching the business
  domain the tool serves.
- Dates as ISO-8601 text (`YYYY-MM-DD`); `date_fin IS NULL` means
  "ongoing".
- Numbered SQL migrations, never modified after their first application —
  any schema change adds a new file.
- No secrets in the database: the database is what gets backed up, and a
  secret stored in it would end up in every backup copy. External access
  keys (S3, LDAP) come only from environment variables.
- No network dependency at startup: the application starts even if the
  configured S3 endpoint or LDAP directory is unreachable.
- The expression engine (`internal/capacity/expr.go`) and the S3 and LDAP
  clients are hand-written, with no third-party library, to stay auditable
  and buildable offline.

## Load testing

A performance test simulates ten years of usage (several thousand servers,
tens of thousands of log entries) and flags any query that would scan a
whole table instead of using an index. It is skipped by default:

```sh
PARALLAX_PERF=1 go test ./internal/web -run TestPerformanceDixAns -v
```

## Publishing a release

Pushing a `vX.Y.Z` tag to the repository triggers
`.github/workflows/release.yml`: it replays `make check`, builds the three
workstation binaries (`make release-tous`: Linux x64, Windows x64, macOS
arm64) and attaches them to the corresponding GitHub release. Nothing else
to do by hand beyond the tag.

## Contribution style

A commit corresponds to one coherent, tested change. Commit messages explain
why, not just what — see the project history for the expected tone.
