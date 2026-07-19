-- +goose Up

-- Per-stage opt-in to capturing request and response content. Off by default,
-- since content is bulky and can be sensitive. When true, the proxy stores the
-- stage's input and output for that stage's completions in completion_io.
ALTER TABLE stages ADD COLUMN capture_io BOOLEAN NOT NULL DEFAULT false;

-- Captured request and response content, one row per captured completion. Kept
-- separate from the lean completion_records so it carries its own retention and
-- access control, and so the metadata table and its queries are untouched.
-- Linked by completion_id with no foreign key, since both are written async and
-- a hard constraint would be fragile under out-of-order flushes. workflow_run
-- and stage_id are denormalized so a whole run's I/O is one indexed query.
CREATE TABLE completion_io (
    completion_id    TEXT PRIMARY KEY,
    workflow_run     TEXT,
    stage_id         TEXT NOT NULL,
    request_content  TEXT,
    response_content TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_completion_io_workflow
    ON completion_io(workflow_run) WHERE workflow_run IS NOT NULL;

-- +goose Down

DROP TABLE completion_io;
ALTER TABLE stages DROP COLUMN capture_io;
