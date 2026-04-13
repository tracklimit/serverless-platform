package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Deployment struct {
	ID              int64      `json:"id"`
	FunctionID      int64      `json:"function_id"`
	FunctionName    string     `json:"function_name"`
	WorkspaceID     int64      `json:"workspace_id"`
	Status          string     `json:"status"`
	KnativeRevision string     `json:"knative_revision"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

func (d *DB) CreateDeployment(ctx context.Context, functionID, workspaceID int64) (*Deployment, error) {
	dep := &Deployment{}
	err := d.pool.QueryRowContext(ctx,
		`INSERT INTO deployments (function_id, workspace_id)
              VALUES ($1, $2)
           RETURNING id, function_id, workspace_id, status, created_at`,
		functionID, workspaceID,
	).Scan(&dep.ID, &dep.FunctionID, &dep.WorkspaceID, &dep.Status, &dep.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create deployment: %w", err)
	}
	return dep, nil
}

func (d *DB) GetDeployment(ctx context.Context, id int64) (*Deployment, error) {
	dep := &Deployment{}
	var knativeRev, errMsg sql.NullString
	var startedAt, completedAt *time.Time

	err := d.pool.QueryRowContext(ctx,
		`SELECT d.id, d.function_id, f.name, d.workspace_id, d.status,
                COALESCE(d.knative_revision, ''), COALESCE(d.error, ''),
                d.created_at, d.started_at, d.completed_at
           FROM deployments d
           JOIN functions f ON f.id = d.function_id
          WHERE d.id = $1`, id,
	).Scan(&dep.ID, &dep.FunctionID, &dep.FunctionName, &dep.WorkspaceID,
		&dep.Status, &knativeRev, &errMsg, &dep.CreatedAt, &startedAt, &completedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	dep.KnativeRevision = knativeRev.String
	dep.Error = errMsg.String
	dep.StartedAt = startedAt
	dep.CompletedAt = completedAt
	return dep, nil
}

func (d *DB) UpdateDeploymentStatus(ctx context.Context, id int64, status string) error {
	query := `UPDATE deployments SET status = $2`
	switch status {
	case "building":
		query += `, started_at = NOW()`
	case "running", "failed":
		query += `, completed_at = NOW()`
	}
	query += ` WHERE id = $1`

	_, err := d.pool.ExecContext(ctx, query, id, status)
	return err
}

func (d *DB) UpdateDeploymentError(ctx context.Context, id int64, errMsg string) error {
	_, err := d.pool.ExecContext(ctx,
		`UPDATE deployments SET status = 'failed', error = $2, completed_at = NOW() WHERE id = $1`,
		id, errMsg,
	)
	return err
}

func (d *DB) UpdateDeploymentRevision(ctx context.Context, id int64, revision string) error {
	_, err := d.pool.ExecContext(ctx,
		`UPDATE deployments SET knative_revision = $2 WHERE id = $1`,
		id, revision,
	)
	return err
}

// ListDeploymentsParams configures pagination for deployment queries.
type ListDeploymentsParams struct {
	FunctionID  int64
	WorkspaceID int64
	Limit       int
	Offset      int
}

func (d *DB) ListDeployments(ctx context.Context, p ListDeploymentsParams) ([]*Deployment, int, error) {
	var total int
	err := d.pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM deployments WHERE function_id = $1 AND workspace_id = $2`,
		p.FunctionID, p.WorkspaceID,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count deployments: %w", err)
	}

	rows, err := d.pool.QueryContext(ctx,
		`SELECT d.id, d.function_id, f.name, d.workspace_id, d.status,
                COALESCE(d.knative_revision, ''), COALESCE(d.error, ''),
                d.created_at, d.started_at, d.completed_at
           FROM deployments d
           JOIN functions f ON f.id = d.function_id
          WHERE d.function_id = $1 AND d.workspace_id = $2
          ORDER BY d.created_at DESC
          LIMIT $3 OFFSET $4`,
		p.FunctionID, p.WorkspaceID, p.Limit, p.Offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var deps []*Deployment
	for rows.Next() {
		dep := &Deployment{}
		var knativeRev, errMsg sql.NullString
		if err := rows.Scan(&dep.ID, &dep.FunctionID, &dep.FunctionName,
			&dep.WorkspaceID, &dep.Status, &knativeRev, &errMsg,
			&dep.CreatedAt, &dep.StartedAt, &dep.CompletedAt); err != nil {
			return nil, 0, err
		}
		dep.KnativeRevision = knativeRev.String
		dep.Error = errMsg.String
		deps = append(deps, dep)
	}
	return deps, total, rows.Err()
}

func (d *DB) ListActiveDeployments(ctx context.Context) ([]*Deployment, error) {
	rows, err := d.pool.QueryContext(ctx,
		`SELECT d.id, d.function_id, f.name, d.workspace_id, d.status,
                COALESCE(d.knative_revision, ''), COALESCE(d.error, ''),
                d.created_at, d.started_at, d.completed_at
           FROM deployments d
           JOIN functions f ON f.id = d.function_id
          WHERE d.status IN ('queued', 'building', 'deploying')
          ORDER BY d.created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active deployments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var deps []*Deployment
	for rows.Next() {
		dep := &Deployment{}
		var knativeRev, errMsg sql.NullString
		if err := rows.Scan(&dep.ID, &dep.FunctionID, &dep.FunctionName,
			&dep.WorkspaceID, &dep.Status, &knativeRev, &errMsg,
			&dep.CreatedAt, &dep.StartedAt, &dep.CompletedAt); err != nil {
			return nil, err
		}
		dep.KnativeRevision = knativeRev.String
		dep.Error = errMsg.String
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}
