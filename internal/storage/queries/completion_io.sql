-- name: CompletionIOByWorkflowRun :many
-- Captured request and response content for every captured stage of one
-- workflow run, in stage order. Written only for stages with capture_io on.
SELECT completion_id, workflow_run, stage_id, request_content, response_content, created_at
FROM completion_io
WHERE workflow_run = $1
ORDER BY created_at;
