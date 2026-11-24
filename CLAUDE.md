# CLAUDE.md — loafkin

loafkin is a shared request board for groups who shop for each other.
One Go binary serves the pages, a JSON API and live updates over one
PostgreSQL database (D-17, D-18, D-23). Why it is built the way it is
lives in `docs/architecture.md`, one decision per `D-NN`; read it before
changing anything it covers. The source is public under the Apache
License 2.0 (D-05), and everything committed stays public for good.

## Commands

- `make hooks`, once per clone, points git at `.githooks`. Its
  pre-commit hook runs `make leaks-staged` on the staged changes, and
  its commit-msg hook runs `make leaks-msg` on the message.
- `make check` runs gofmt, go vet, the tests and govulncheck, as CI
  does. Database tests skip unless `LOAFKIN_TEST_DATABASE_URL` names a
  scratch PostgreSQL.
- `make leaks` runs gitleaks over the working tree, the full history and
  every commit message, as CI does.
- `make run` serves from source on `127.0.0.1:8080`, against the local
  PostgreSQL that `make db` starts from `compose.yaml`. `make run-image`
  serves the production image from the `Dockerfile` instead. `make
  db-stop` removes the database and its data. Podman is the default;
  set `CONTAINER=docker` for Docker.

## Hard rules

1. **Keep everything internal out of the repository** (D-39): secrets,
   production data, personal details and infrastructure identifiers stay
   out of code, docs, fixtures, commit messages, pull requests and CI
   output, because everything committed is public for good.
   - Secrets, such as OAuth client secrets, Apple's `.p8` key, database
     passwords, session keys and API keys, live in a secret manager and
     reach the app through its environment or files at run time.
   - Test data is generated. Dumps, seeds, logs, screenshots and support
     mail from production stay out.
   - Account and project ids, billing and registrar details stay out.
   - Compliance paperwork stays private: the impact assessment, the
     record of processing, signed processor agreements and the breach
     log. The privacy policy is the public part.
   - An unfixed security issue goes through private vulnerability
     reporting (`SECURITY.md`), not an issue or a commit message.
   - A rule that applies here is written here, in full, rather than as a
     pointer to a maintainer's machine or private files.
   - CI output carries no secret, because a public repository's Actions
     logs are public too.
   - The copyright line in `NOTICE` and `LICENSE` is the one personal
     detail the repository carries.

   `make leaks` and the hooks hold it. A test value uses the synthetic
   forms `.gitleaks.toml` allows. The gitleaks version is pinned in the
   `Makefile` and nowhere else.
2. **Commit and push only on a branch, open a pull request, and leave
   merging and tagging to the owner** (D-46). Every change reaches
   `main` through a pull request, because the owner reads everything
   before it enters `main`.
3. **Keep personal data in the EU**: every host, email sender and script
   that touches it runs in the EU, and pages load nothing from a CDN
   (D-07).
4. **Log the route pattern, the outcome, the duration, a request id and
   a keyed hash of the member, and log an error only as its codes.**
   Request paths, query strings, cookies, `Authorization` headers,
   bodies and raw error text stay out of logs, because they carry tokens
   and personal data (D-33, D-34).
5. **Read and write data only through a store method scoped to the
   caller, and register every route with its permission** (D-21, D-22).
6. **Change state only on POST and other unsafe methods, sign-in aside,
   and keep cross-origin protection on every route except Apple's
   callback.** The `state` and nonce of each sign-in attempt protect the
   sign-in start link and both callbacks (D-14).
7. **Publish a live update only from the transaction that commits the
   change**, so a rollback announces nothing (D-27).
8. **Store session tokens and API tokens only as hashes** (D-13, D-15).
   Sign-in stays passwordless (D-10).
9. **Serve browser code from loafkin's own embedded files: vendored
   libraries pinned by hash, scripts in files rather than inline or
   `hx-on`, and no Node toolchain** (D-31, D-32).
10. **Fix a migration that has run in production forward, with a new
    migration, rather than by editing it** (D-25). Write SQL by hand on
    pgx, without an ORM or a SQL code generator (D-24).
11. **Answer every error with loafkin's own codes and a generic
    message.** Responses, pages and emails carry no stack trace, SQL,
    internal id, file path or upstream error text. Something the caller
    may not see answers 404, like something that does not exist, and
    sign-in answers the same whether or not an account exists (D-33).
12. **Carry a token in a link's fragment, not in its path or query
    string** (D-42). Expose only random public ids, not row ids (D-21).
13. **Render each live update as the stream's member, after re-checking
    the session and the membership** (D-27).
14. **Select an account by an identity's issuer and subject, not by an
    email address from Google or Apple** (D-44).

## Definition of done

- `make check` passes with `LOAFKIN_TEST_DATABASE_URL` set.
- `make leaks` passes.
- A user-visible change, the JSON API included, has one line under
  `[Unreleased]` in `CHANGELOG.md`.
- `/simplify` and `/code-review` have run; the owner starts
  `/code-review`, because Claude does not run it on its own. Ask for it
  when a change is ready. A fixed finding is one line
  in the commit body. A declined one goes in the PR body, or becomes a
  present-tense constraint comment at the line it protects. Findings
  stay out of `docs/`.

## Traps

- **A leak scan cannot see personal data written in ordinary words.**
  A name or an address has no shape gitleaks can match, so test data is
  generated rather than copied from anywhere.
- **A decision is superseded or deprecated, never deleted, and an ID is
  never reused.** Take the next ID from the `Next ID:` line in
  `docs/architecture.md`.
- **A recommendation is not a decision.** Research or Claude can
  propose; only the owner decides. Write a proposal into the docs or the
  code only after the owner has decided it.

## Writing

`CONTRIBUTING.md` holds the writing, commit and CHANGELOG rules; follow
them in everything written here, this file included.
