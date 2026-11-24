-- +goose Up
SET LOCAL lock_timeout = '5s';

-- An email waits here until a worker sends it (D-29). The row holds the
-- address and the body, so it is deleted once sent, once it expires, or
-- with its account.
CREATE TABLE outbox (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id      bigint REFERENCES accounts (id) ON DELETE CASCADE,
    recipient       text NOT NULL,
    subject         text NOT NULL,
    body            text NOT NULL,
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL
);

CREATE INDEX outbox_next_attempt_at ON outbox (next_attempt_at);
CREATE INDEX outbox_account_id ON outbox (account_id);
CREATE INDEX sessions_expires_at ON sessions (expires_at);
CREATE INDEX sessions_last_used_at ON sessions (last_used_at);

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP INDEX sessions_last_used_at;
DROP INDEX sessions_expires_at;
DROP TABLE outbox;
