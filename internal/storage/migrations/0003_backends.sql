CREATE TABLE backends (
    name              TEXT PRIMARY KEY,
    endpoint          TEXT NOT NULL,
    model_id          TEXT NOT NULL,
    api_key_env_var   TEXT NOT NULL DEFAULT '',
    max_concurrency   INTEGER,
    queue_capacity    INTEGER,
    input_cost_per_mtoken  REAL,
    output_cost_per_mtoken REAL,
    quality           REAL,
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);
