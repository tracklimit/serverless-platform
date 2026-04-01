package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

type Workspace struct {
	ID        int64
	Slug      string
	Name      string
	CreatedAt time.Time
}

type WorkspaceMember struct {
	WorkspaceID int64
	UserID      int64
	Username    string
	Role        string
}

type DB struct {
	pool *sql.DB
}

func New(dsn string) (*DB, error) {
	pool, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(5 * time.Minute)

	if err := pool.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (d *DB) Close() error { return d.pool.Close() }

// Users

func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	err := d.pool.QueryRowContext(ctx,
		`SELECT id, username, password_hash, is_admin, created_at
		   FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (d *DB) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (*User, error) {
	u := &User{}
	err := d.pool.QueryRowContext(ctx,
		`INSERT INTO users (username, password_hash, is_admin)
		      VALUES ($1, $2, $3)
		   RETURNING id, username, password_hash, is_admin, created_at`,
		username, passwordHash, isAdmin,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (d *DB) UpdatePasswordHash(ctx context.Context, username, newHash string) error {
	_, err := d.pool.ExecContext(ctx,
		`UPDATE users SET password_hash = $1 WHERE username = $2`,
		newHash, username,
	)
	return err
}

// ListUsersParams configures pagination for user queries.
type ListUsersParams struct {
	Limit  int
	Offset int
}

func (d *DB) ListUsers(ctx context.Context, p ListUsersParams) ([]*User, int, error) {
	var total int
	if err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	rows, err := d.pool.QueryContext(ctx,
		`SELECT id, username, is_admin, created_at FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		p.Limit, p.Offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (d *DB) DeleteUser(ctx context.Context, username string) error {
	res, err := d.pool.ExecContext(ctx,
		`DELETE FROM users WHERE username = $1`, username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (d *DB) HasAnyUser(ctx context.Context) (bool, error) {
	var count int
	err := d.pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

// Workspaces

func (d *DB) CreateWorkspace(ctx context.Context, slug, name string) (*Workspace, error) {
	w := &Workspace{}
	err := d.pool.QueryRowContext(ctx,
		`INSERT INTO workspaces (slug, name)
		      VALUES ($1, $2)
		   RETURNING id, slug, name, created_at`,
		slug, name,
	).Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	return w, nil
}

func (d *DB) GetWorkspaceBySlug(ctx context.Context, slug string) (*Workspace, error) {
	w := &Workspace{}
	err := d.pool.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at FROM workspaces WHERE slug = $1`, slug,
	).Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return w, nil
}

// ListWorkspacesParams configures pagination and sorting for workspace queries.
type ListWorkspacesParams struct {
	Limit  int
	Offset int
	Sort   string
	Order  string
}

func sanitizeWorkspaceSort(col string) string {
	switch col {
	case "name":
		return "name"
	case "slug":
		return "slug"
	default:
		return "created_at"
	}
}

func (d *DB) ListWorkspaces(ctx context.Context, p ListWorkspacesParams) ([]*Workspace, int, error) {
	var total int
	if err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM workspaces").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count workspaces: %w", err)
	}

	query := fmt.Sprintf( //nolint:gosec // sort/order values are allowlisted by sanitizeWorkspaceSort and sanitizeOrder
		`SELECT id, slug, name, created_at FROM workspaces ORDER BY %s %s LIMIT $1 OFFSET $2`,
		sanitizeWorkspaceSort(p.Sort), sanitizeOrder(p.Order),
	)

	rows, err := d.pool.QueryContext(ctx, query, p.Limit, p.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var workspaces []*Workspace
	for rows.Next() {
		w := &Workspace{}
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt); err != nil {
			return nil, 0, err
		}
		workspaces = append(workspaces, w)
	}
	return workspaces, total, rows.Err()
}

func (d *DB) DeleteWorkspace(ctx context.Context, slug string) error {
	res, err := d.pool.ExecContext(ctx,
		`DELETE FROM workspaces WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace not found")
	}
	return nil
}

// Workspace members

func (d *DB) AddWorkspaceMember(ctx context.Context, workspaceSlug, username, role string) error {
	_, err := d.pool.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role)
		 SELECT w.id, u.id, $3
		   FROM workspaces w, users u
		  WHERE w.slug = $1 AND u.username = $2
		 ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = $3`,
		workspaceSlug, username, role,
	)
	return err
}

func (d *DB) RemoveWorkspaceMember(ctx context.Context, workspaceSlug, username string) error {
	res, err := d.pool.ExecContext(ctx,
		`DELETE FROM workspace_members
		  WHERE workspace_id = (SELECT id FROM workspaces WHERE slug = $1)
		    AND user_id      = (SELECT id FROM users WHERE username = $2)`,
		workspaceSlug, username,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("member not found")
	}
	return nil
}

func (d *DB) ListWorkspaceMembers(ctx context.Context, workspaceSlug string) ([]*WorkspaceMember, error) {
	rows, err := d.pool.QueryContext(ctx,
		`SELECT wm.workspace_id, wm.user_id, u.username, wm.role
		   FROM workspace_members wm
		   JOIN users u ON u.id = wm.user_id
		   JOIN workspaces w ON w.id = wm.workspace_id
		  WHERE w.slug = $1
		  ORDER BY wm.role DESC, u.username`,
		workspaceSlug,
	)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var members []*WorkspaceMember
	for rows.Next() {
		m := &WorkspaceMember{}
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Username, &m.Role); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// Functions

type Function struct {
	ID          int64
	WorkspaceID int64
	Name        string
	Runtime     string
	DeployType  string
	Code        string
	Image       string
	Public      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (d *DB) CreateFunction(ctx context.Context, workspaceID int64, name, runtime, deployType, code, image string, public bool) (*Function, error) {
	f := &Function{}
	err := d.pool.QueryRowContext(ctx,
		`INSERT INTO functions (workspace_id, name, runtime, deploy_type, code, image, public)
		      VALUES ($1, $2, $3, $4, $5, $6, $7)
		   RETURNING id, workspace_id, name, runtime, deploy_type, COALESCE(code,''), COALESCE(image,''), public, created_at, updated_at`,
		workspaceID, name, runtime, deployType, nullableString(code), nullableString(image), public,
	).Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Runtime, &f.DeployType, &f.Code, &f.Image, &f.Public, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create function: %w", err)
	}
	return f, nil
}

func (d *DB) GetFunction(ctx context.Context, workspaceID int64, name string) (*Function, error) {
	f := &Function{}
	err := d.pool.QueryRowContext(ctx,
		`SELECT id, workspace_id, name, runtime, deploy_type, COALESCE(code,''), COALESCE(image,''), public, created_at, updated_at
		   FROM functions WHERE workspace_id = $1 AND name = $2`,
		workspaceID, name,
	).Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Runtime, &f.DeployType, &f.Code, &f.Image, &f.Public, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get function: %w", err)
	}
	return f, nil
}

// ListFunctionsParams configures pagination, filtering, and sorting for function queries.
type ListFunctionsParams struct {
	WorkspaceID int64
	Limit       int
	Offset      int
	Sort        string
	Order       string
	Runtime     string
	DeployType  string
}

func sanitizeFunctionSort(col string) string {
	switch col {
	case "name":
		return "name"
	case "updated_at":
		return "updated_at"
	default:
		return "created_at"
	}
}

func sanitizeOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "ASC"
	}
	return "DESC"
}

func (d *DB) ListFunctions(ctx context.Context, p ListFunctionsParams) ([]*Function, int, error) {
	conditions := []string{"workspace_id = $1"}
	args := []any{p.WorkspaceID}
	argIdx := 2

	if p.Runtime != "" {
		conditions = append(conditions, fmt.Sprintf("runtime = $%d", argIdx))
		args = append(args, p.Runtime)
		argIdx++
	}
	if p.DeployType != "" {
		conditions = append(conditions, fmt.Sprintf("deploy_type = $%d", argIdx))
		args = append(args, p.DeployType)
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM functions "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count functions: %w", err)
	}

	query := fmt.Sprintf( //nolint:gosec // sort/order values are allowlisted by sanitizeFunctionSort and sanitizeOrder
		`SELECT id, workspace_id, name, runtime, deploy_type, COALESCE(code,''), COALESCE(image,''), public, created_at, updated_at
		 FROM functions %s ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		where, sanitizeFunctionSort(p.Sort), sanitizeOrder(p.Order), argIdx, argIdx+1,
	)
	args = append(args, p.Limit, p.Offset)

	rows, err := d.pool.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list functions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var functions []*Function
	for rows.Next() {
		f := &Function{}
		if err := rows.Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Runtime, &f.DeployType, &f.Code, &f.Image, &f.Public, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, 0, err
		}
		functions = append(functions, f)
	}
	return functions, total, rows.Err()
}

func (d *DB) UpdateFunction(ctx context.Context, workspaceID int64, name, code, image string, public bool) (*Function, error) {
	f := &Function{}
	err := d.pool.QueryRowContext(ctx,
		`UPDATE functions
		    SET code = $3, image = $4, public = $5, updated_at = NOW()
		  WHERE workspace_id = $1 AND name = $2
		  RETURNING id, workspace_id, name, runtime, deploy_type, COALESCE(code,''), COALESCE(image,''), public, created_at, updated_at`,
		workspaceID, name, nullableString(code), nullableString(image), public,
	).Scan(&f.ID, &f.WorkspaceID, &f.Name, &f.Runtime, &f.DeployType, &f.Code, &f.Image, &f.Public, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update function: %w", err)
	}
	return f, nil
}

func (d *DB) DeleteFunction(ctx context.Context, workspaceID int64, name string) error {
	res, err := d.pool.ExecContext(ctx,
		`DELETE FROM functions WHERE workspace_id = $1 AND name = $2`,
		workspaceID, name,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("function not found")
	}
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// GetUserWorkspace returns the workspace the user belongs to (first membership found).
func (d *DB) GetUserWorkspace(ctx context.Context, username string) (*Workspace, string, error) {
	w := &Workspace{}
	var role string
	err := d.pool.QueryRowContext(ctx,
		`SELECT ws.id, ws.slug, ws.name, ws.created_at, wm.role
		   FROM workspaces ws
		   JOIN workspace_members wm ON wm.workspace_id = ws.id
		   JOIN users u ON u.id = wm.user_id
		  WHERE u.username = $1
		  LIMIT 1`,
		username,
	).Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt, &role)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("get user workspace: %w", err)
	}
	return w, role, nil
}
