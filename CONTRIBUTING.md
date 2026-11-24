# Contributing

How to work on loafkin. Why it is built the way it is lives in
[`docs/architecture.md`](docs/architecture.md), one decision per `D-NN`;
read the entries a change touches before making it. A security issue
never goes in an issue or a pull request:
[`SECURITY.md`](SECURITY.md) says where it goes.

## Setting up

- Install Go at the version `go.mod` names.
- Run `make hooks` once per clone. It points git at `.githooks`, whose
  pre-commit hook runs `make leaks-staged` on the staged changes.
- Install Podman with its compose command, or Docker with
  `CONTAINER=docker` set, to run the server locally.

## Running locally

- `make run` serves from source on `127.0.0.1:8080`. It first starts a
  local PostgreSQL from `compose.yaml` with `make db`, and the server
  applies the migrations at startup (D-25).
- `make run-image` builds the production image from the `Dockerfile`
  and serves it on the same address, against the same database.
- `make db-stop` removes the database and its data.

## Before a pull request

- `make check` passes. It runs gofmt, go vet, the tests and govulncheck,
  as CI does. Database tests need `LOAFKIN_TEST_DATABASE_URL` set to a
  scratch PostgreSQL, and skip without it; CI always sets it (D-35).
- `make leaks` passes. It runs gitleaks over the working tree and the
  full history, as CI does.
- Nothing internal enters the repository: no secret, no production
  data, no personal detail and no infrastructure identifier (D-39). Test
  data is generated, never copied from anywhere. A test credential uses
  the synthetic forms `.gitleaks.toml` allows.
- A changed decision is a new entry that supersedes the old one, never
  an edit that hides it. IDs are never reused: take the next one from
  the `Next ID:` line.
- Every change reaches `main` through a pull request that the owner
  merges (D-46).

## Commit messages

- The subject is 72 characters at most, and aims for 50.
- The body says what the problem was, why this change solves it, and
  which alternatives were rejected. A long body is a sign the change
  should be split.
- A fixed review finding is one line at the end of the body.

## CHANGELOG

- A user-visible change, the JSON API included, adds one line under
  `[Unreleased]` in [`CHANGELOG.md`](CHANGELOG.md), in Keep a
  Changelog's sections: Added, Changed, Deprecated, Removed, Fixed,
  Security.
- Each line is 25 words or fewer and leads with the effect on the user.
  A change the reader must act on starts with `**Breaking:**` and may
  add one sentence saying what to do. A fix names the CVE or GHSA it
  closes.
- A dependency bump needs no line unless it fixes an advisory.

## Writing

- Write American English everywhere a person reads.
- Lead with the point. Keep one idea per sentence, and 25 words or fewer
  as a rule.
- Docs describe loafkin as it is. No dates, no process or phase notes,
  no research logs and no record of who suggested what. History goes in
  commit messages.
- Give each fact one home, and cite it elsewhere by its D-ID or a
  `docs/<file>.md#<slug>` link.
- Record only decisions the owner has made. A proposal stays in its
  issue or pull request until then.
- A feature's goals and success criteria go in its issue.
- A code comment says why, in one to three lines.
