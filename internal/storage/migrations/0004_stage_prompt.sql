-- +goose Up

-- The stage's prompt override. When non-empty, the proxy substitutes it
-- for the stage's leading system message on every call tagged with the
-- stage. Empty is the "not set" state, so the client's own prompt passes
-- through untouched.
ALTER TABLE stages ADD COLUMN prompt TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE stages DROP COLUMN prompt;
