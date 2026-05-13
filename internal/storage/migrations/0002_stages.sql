-- +goose Up
CREATE TABLE stages (
    id                          TEXT PRIMARY KEY,
    backend                     TEXT NOT NULL DEFAULT '',
    scheduling_policy           TEXT NOT NULL DEFAULT '',
    request_scheduling_policy   TEXT NOT NULL DEFAULT '',
    priority                    INTEGER,
    request_priority            INTEGER,
    max_concurrency             INTEGER,
    reasoning_effort            TEXT NOT NULL DEFAULT '',
    flush_on_complete           INTEGER NOT NULL DEFAULT 0,
    reasoning_hint              TEXT NOT NULL DEFAULT '',
    labels_json                 TEXT NOT NULL DEFAULT '{}',
    created_at                  INTEGER NOT NULL,
    updated_at                  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE stages;
