CREATE TABLE admins (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE admin_sessions (
    token_hash BLOB PRIMARY KEY,
    admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX admin_sessions_admin_id_idx ON admin_sessions(admin_id);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions(expires_at);

CREATE TABLE queue (
    id INTEGER PRIMARY KEY,
    data BLOB NOT NULL,
    reference TEXT NOT NULL,
    enqueued_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    unavailable_until TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    processed_at TEXT
);

CREATE INDEX queue_pending_idx
ON queue(reference, unavailable_until, enqueued_at, id)
WHERE processed_at IS NULL;

CREATE TABLE blobs (
    key TEXT PRIMARY KEY,
    meta TEXT NOT NULL,
    data BLOB NOT NULL DEFAULT X'',
    large INTEGER NOT NULL CHECK (large IN (0, 1))
);
