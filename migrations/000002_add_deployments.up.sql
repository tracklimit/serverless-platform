CREATE TYPE deploy_status AS ENUM (
    'queued',
    'building',
    'deploying',
    'running',
    'failed'
);

CREATE TABLE deployments (
                             id              BIGSERIAL PRIMARY KEY,
                             function_id     BIGINT NOT NULL REFERENCES functions(id) ON DELETE CASCADE,
                             workspace_id    BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
                             status          deploy_status NOT NULL DEFAULT 'queued',
                             knative_revision VARCHAR(255),
                             error           TEXT,
                             created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                             started_at      TIMESTAMPTZ,
                             completed_at    TIMESTAMPTZ
);

CREATE INDEX idx_deployments_function_id ON deployments (function_id, created_at DESC);
CREATE INDEX idx_deployments_workspace_id ON deployments (workspace_id);
CREATE INDEX idx_deployments_status ON deployments (status) WHERE status IN ('queued', 'building', 'deploying');