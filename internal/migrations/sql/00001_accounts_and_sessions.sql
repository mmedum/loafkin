-- +goose Up
SET LOCAL lock_timeout = '5s';

CREATE TABLE accounts (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A session is found by the SHA-256 hash of its token; the token itself
-- is never stored (D-13).
CREATE TABLE sessions (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   bigint NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL
);

CREATE INDEX sessions_account_id ON sessions (account_id);

-- +goose Down
SET LOCAL lock_timeout = '5s';

DROP TABLE sessions;
DROP TABLE accounts;
