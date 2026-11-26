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

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP TABLE outbox;
