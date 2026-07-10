-- +goose Up

-- The stage's prompt override. When non-empty, the proxy substitutes it
-- for the leading instruction message, the system or developer message,
-- on every call tagged with the stage. Empty is the "not set" state, so
-- the client's own prompt passes through untouched.
ALTER TABLE stages ADD COLUMN prompt TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE stages DROP COLUMN prompt;
