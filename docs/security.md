# Security model

The threat model and the invariants loafkin holds.
[`SECURITY.md`](../SECURITY.md) says how to report a vulnerability.

loafkin is a website open to anyone on the internet. One Go binary
serves the pages, the JSON API and live updates, and keeps every
person's data in one PostgreSQL database (D-17, D-23). Members sign in
without a password (D-10) and see only the groups they belong to
(D-04).

## Trust boundaries

| Boundary | Crosses | Assumption |
| --- | --- | --- |
| Browser ↔ loafkin | HTTPS | Untrusted. A session is a server-side record behind a `__Host-` cookie (D-13). Every state-changing request signed in by that cookie passes cross-origin protection, except Apple's callback (D-14), and every response carries a strict Content Security Policy (D-32). |
| API client ↔ loafkin | HTTPS, with a personal token | Untrusted. A token acts for one member, is stored only as a hash and expires (D-15). |
| loafkin ↔ PostgreSQL | The host's network | The store, the migrations and the live-update listener are the only code that talks to the database. Every store method takes a caller, a member or the system identity background work runs as, and is scoped to it (D-21). |
| loafkin ↔ Google and Apple | OpenID Connect over HTTPS | An ID token counts only after its signature, issuer, audience and nonce check out (D-11). |
| loafkin ↔ the email sender | SMTP over TLS (D-47) | It receives an address and a sign-in code or a join notice, and it sends from the EU (D-07). The member's own mailbox is outside loafkin's control. |
| One member ↔ another | Item text, notes and photos | Written by other people. Templates escape all text (D-18), and the server re-encodes every photo (D-30). |
| loafkin ↔ the phone | The service worker's cache and IndexedDB | The last list and any queued "bought" stay on the device until sign-out (D-28). |

## What is worth stealing

- **A session or an API token.** Either acts as the member until it
  expires or is revoked.
- **A member's email inbox.** An emailed code signs in, so whoever reads
  the inbox can sign in, unless the member has turned email sign-in off.
- **A group's contents.** Who needs what reveals a household's life:
  health items, routines, who lives where.
- **Photos.** A photo can carry a location and a time until the server
  re-encodes it (D-30).
- **Membership.** That a group exists, and who is in it, is itself
  private (D-04).

## Adversaries

**Anyone on the internet, without an account.** They can guess at
invite links and sign-in codes, and probe for groups and accounts. An
invite link is a long random token, and the invite page shows only the
group's name and who invited, never its members or requests (D-04).
Sign-in codes are single-use, expire within minutes, work only in the
browser that asked for them (D-10), and sit behind rate limits kept in
PostgreSQL (D-43). Asking for a code gets the same answer whether or
not the address has an account, and a group the caller may not see
answers like one that does not exist (D-33).

**A member of another group.** They are signed in, so authentication
does not stop them. Every route declares the permission it needs (D-22),
every store method takes the caller and returns only what that caller
may see (D-21), and a photo is served only after the same group check
(D-30). Public ids are random, and a generated test calls every id
route as another group's member and expects a 404 (D-21, D-22).

**A member who was removed, or whose session was revoked.** Their open
live streams close, and every push re-checks the session and the
membership first (D-27).

**Someone who reaches a signed-in session briefly**, on an unlocked
phone, for one. Creating an API token needs a fresh sign-in, and so do
exporting, deleting and changing how the member signs in (D-10, D-15).

**A member of the same group.** By design, they see the group's
requests, staples and history, and can claim and mark items bought.
What each role may change is enforced below the handlers, so the pages
and the JSON API cannot drift apart (D-21, D-22).

**A hostile site a member visits.** It can make the browser send
requests with the member's cookie. Cross-origin protection rejects every
state-changing request that is not from loafkin, and no GET outside
sign-in changes a member's data (D-14). Script injected through item
text fails twice: templates escape it (D-18), and the Content Security
Policy runs no inline script (D-32).

**The next person on a shared phone.** The offline list and the queued
purchases belong to one account. Signing out, an expired session and a
change of account delete them, and the server refuses a queued mark
sent under another account (D-28).

**Whoever finds a leaked API token.** Its loafkin prefix lets a secret
scanner be set up to recognize it, it expires within 365 days, and the
member can revoke it, or sign out everywhere. It cannot create tokens,
export, delete the account or change how its owner signs in (D-13,
D-15).

**Whoever reads a log, the host's included.** No URL carries a token,
PostgreSQL logs no query parameters, and every log stays in the EU for
at most 45 days (D-34, D-42).

## Invariants

A way to break one of these is a vulnerability.

- Only a group's members can see that it exists, or anything in it
  (D-04, D-21, D-22).
- No password is stored, because there are none (D-10). Session tokens
  and API tokens are stored only as hashes (D-13, D-15).
- No GET outside sign-in changes a member's data. Every other request
  signed in by the session cookie passes cross-origin protection, except
  Apple's callback. The `state` and nonce of each sign-in attempt
  protect the sign-in start link and both callbacks (D-14).
- Every response carries the Content Security Policy, and no page runs
  inline script (D-32).
- A live update announces only a change that committed, and a stream
  sends only what its member may see at the moment it sends (D-27).
- A "bought" sent from the offline queue is applied at most once, and
  only under the account that queued it (D-28).
- Creating an API token needs a fresh sign-in, and a token cannot create
  tokens, export, delete the account or change how its owner signs in
  (D-15).
- An account is never selected by an email address from Google or
  Apple, and an identity is linked only after a fresh sign-in (D-44).
- Public ids are random, never row ids (D-21).
- No URL carries a token, and every log stays in the EU for at most 45
  days (D-42).
- A photo is stored and served without metadata, only to members of its
  group, and leaves the database with its request (D-23, D-30).
- No log line carries a request path, a query string, a cookie, an
  `Authorization` header or a body, and an error reaches a log only as
  codes (D-33, D-34).
- No response, page or email carries a stack trace, SQL, an internal
  id, a file path or an upstream service's error text. Something the
  caller may not see answers exactly like something that does not
  exist, and sign-in never confirms that an account exists (D-33).
- Personal data, photos, backups and email stay in the EU wherever
  loafkin or a service it uses holds them (D-07). Deleted data is gone
  from backups within 30 days, and a restore never brings back a
  deleted account (D-08).
- A cleared request stays in its group's history as text for as long as
  the group exists, except what a deleted account takes with it (D-09).

## Not a vulnerability

Do not report these as vulnerabilities. Open an ordinary issue if one
surprises you.

- **A member sees everything in their group**, including who bought
  what for whom, and the group's history. That is what a group is.
- **An admin approves the wrong person.** Joining needs an admin's
  approval, and the admin's judgment is theirs.
- **Someone with access to your email signs in as you.** Email is a
  sign-in method. Add a passkey and turn email sign-in off to close it.
- **The offline list on a phone that was never signed out.**
- **Someone else's code.** A flaw in Google's or Apple's sign-in, the
  email sender, the host or a dependency goes to that project. A way to
  make loafkin itself break an invariant through one is a vulnerability.

## Dependency vulnerabilities

A CVE in a dependency is a vulnerability in loafkin only when loafkin's
code can reach the vulnerable symbol, as `govulncheck` reports it. Such
a fix ships in a release whose CHANGELOG entry names the CVE or GHSA.
Any other advisory is handled by updating the dependency in the normal
course.

## What you are responsible for

- **Your devices.** Sign out on a shared phone or computer, because the
  offline list stays until you do.
- **Your email account and passkeys.** They are how you sign in.
- **Whom you approve and make admin.** An admin can remove members and
  requests, and approve others.
- **What you write and photograph.** Every member of the group sees
  item text, notes and photos.
- **Your API tokens.** Keep them out of shared files and chat, give each
  the shortest expiry that works, and revoke one you stop using.
