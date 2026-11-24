# loafkin

[![CI](https://github.com/mmedum/loafkin/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mmedum/loafkin/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/mmedum/loafkin?sort=semver&include_prereleases)](https://github.com/mmedum/loafkin/releases/latest)
[![License: Apache 2.0](https://img.shields.io/github/license/mmedum/loafkin)](./LICENSE)

A shared request board for groups who shop for each other. Everyone in a
group sees what each person needs, claims an item before going to the
store, and marks it bought for that person.

It is for any set of people who buy things for one another: a family,
roommates, neighbors, or someone who shops for a parent. A group is not
one household, and a person can be in several groups (D-03). One
shopping list covers all of them. loafkin is one website for phones and
computers (D-02).

This repository holds loafkin's design and decisions, and there is no
release.

## Goals and non-goals

Goals:

- **Two people don't buy the same thing.** A claim shows for everyone
  in the group within a second or two, with the buyer's name (D-27).
  Only a purchase marked offline can cross another, and loafkin then
  says so (D-28).
- **It works in the store.** The list stays readable without a
  connection, and marking an item bought works offline. The mark is
  sent the next time loafkin is open with a connection (D-28).
- **A group is private.** Only its members can see that it exists, and
  an invite shows only the group's name and who invited you until an
  admin approves the join (D-04).
- **Your data stays in the EU, and leaves when you do.** Deleted data
  is gone from backups within 30 days (D-07, D-08).
- **No passwords.** You sign in with Google, Apple, an emailed code or
  a passkey (D-10).

Non-goals:

- **A pantry tracker.** No counted quantities, barcodes or expiry
  dates: households rarely keep counts up to date. A quantity goes in
  the item's text.
- **A store or recipe app.** No store-specific lists, aisle sorting,
  recipes or meal plans.

## Documentation

- [`docs/architecture.md`](docs/architecture.md): how loafkin is built,
  and every decision behind it.
- [`docs/security.md`](docs/security.md): the threat model.
- [`SECURITY.md`](SECURITY.md): how to report a vulnerability.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): how to work on loafkin.
- [`CHANGELOG.md`](CHANGELOG.md): what changed in each release.

## License

Apache License 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
