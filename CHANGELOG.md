# Changelog

All notable changes to loafkin are documented here. The format is
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each
release is a deploy (D-38). The versioning contract is the public JSON
API: removing or renaming an endpoint, a field or an error code, or
adding a required field, is breaking. Pages, package layout and log
lines are not part of it.

## [Unreleased]

### Added

- `serve` sends email over SMTP with TLS and needs `LOAFKIN_SMTP_ADDR` and `LOAFKIN_MAIL_FROM` to start.

[Unreleased]: https://github.com/mmedum/loafkin/commits/main
