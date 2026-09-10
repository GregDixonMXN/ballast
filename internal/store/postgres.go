package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"ballast/internal/changeset"
	"ballast/internal/events"
	"ballast/internal/project"
	"ballast/internal/task"
	"ballast/internal/workspace"
)

// PGRepo persists records in Postgres. Upsert semantics match MemoryRepo.
type PGRepo struct {
	db *sql.DB
}

func NewPGRepo(db *sql.DB) *PGRepo { return &PGRepo{db: db} }

// nullUUID maps "" to SQL NULL, else the UUID string (Postgres casts it).
func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func strList(v []string) string {
	if v == nil {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func parseList(raw []byte, dst *[]string) error {
	if len(raw) == 0 {
		*dst = nil
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// ensureOrg returns the id of the owning org, creating the MVP default
// ("default") when the caller supplies none. Real multi-org scoping
// arrives with OIDC + organizations UI; until then everything belongs
// to one org rather than failing NOT NULL constraints.
func (p *PGRepo) ensureOrg(ctx context.Context, orgID string) (string, error) {
	if orgID != "" {
		return orgID, nil
	}
	var id string
	err := p.db.QueryRowContext(ctx, `SELECT id FROM organizations WHERE name='default'`).Scan(&id)
	if err == sql.ErrNoRows {
		err = p.db.QueryRowContext(ctx,
			`INSERT INTO organizations(name) VALUES('default') RETURNING id`).Scan(&id)
	}
	if err != nil {
		return "", fmt.Errorf("default org: %w", err)
	}
	return id, nil
}

func (p *PGRepo) SaveProject(ctx context.Context, pr project.Project) error {
	org, err := p.ensureOrg(ctx, pr.OrgID)
	if err != nil {
		return err
	}
	_, err = p.db.ExecContext(ctx, `
INSERT INTO projects(id, org_id, name, repo_path, branch, canonical_sha, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, repo_path=EXCLUDED.repo_path,
branch=EXCLUDED.branch, canonical_sha=EXCLUDED.canonical_sha`,
		pr.ID, org, pr.Name, pr.RepoPath, pr.Branch, pr.CanonicalSHA, pr.CreatedAt)
	return err
}

func (p *PGRepo) GetProject(ctx context.Context, id string) (project.Project, error) {
	var pr project.Project
	err := p.db.QueryRowContext(ctx, `
SELECT id, org_id, name, repo_path, branch, canonical_sha, created_at
FROM projects WHERE id=$1`, id).Scan(
		&pr.ID, &pr.OrgID, &pr.Name, &pr.RepoPath, &pr.Branch, &pr.CanonicalSHA, &pr.CreatedAt)
	if err == sql.ErrNoRows {
		return project.Project{}, fmt.Errorf("unknown project %s", id)
	}
	return pr, err
}

func (p *PGRepo) SetCanonicalHead(ctx context.Context, id, sha string) error {
	res, err := p.db.ExecContext(ctx, `UPDATE projects SET canonical_sha=$2 WHERE id=$1`, id, sha)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("unknown project %s", id)
	}
	return nil
}

func (p *PGRepo) SaveTask(ctx context.Context, t task.Task) error {
	_, err := p.db.ExecContext(ctx, `
INSERT INTO tasks(id, project_id, title, description, scopes, status, assignee_id, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, description=EXCLUDED.description,
scopes=EXCLUDED.scopes, status=EXCLUDED.status, assignee_id=EXCLUDED.assignee_id,
updated_at=EXCLUDED.updated_at`,
		t.ID, t.ProjectID, t.Title, t.Description, strList(t.Scopes),
		string(t.Status), nullUUID(t.AssigneeID), t.CreatedAt, t.UpdatedAt)
	return err
}

func (p *PGRepo) DeleteTask(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM tasks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown task %s", id)
	}
	return nil
}

func scanTask(row *sql.Row) (task.Task, error) {
	var t task.Task
	var scopes []byte
	var status string
	var assignee sql.NullString
	err := row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &scopes,
		&status, &assignee, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return task.Task{}, err
	}
	t.Status = task.Status(status)
	if assignee.Valid {
		t.AssigneeID = assignee.String
	}
	if err := parseList(scopes, &t.Scopes); err != nil {
		return task.Task{}, err
	}
	return t, nil
}

func (p *PGRepo) GetTask(ctx context.Context, id string) (task.Task, error) {
	t, err := scanTask(p.db.QueryRowContext(ctx, `
SELECT id, project_id, title, description, scopes, status, assignee_id, created_at, updated_at
FROM tasks WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return task.Task{}, fmt.Errorf("unknown task %s", id)
	}
	return t, err
}

func (p *PGRepo) ListTasks(ctx context.Context, projectID string) ([]task.Task, error) {
	rows, err := p.db.QueryContext(ctx, `
SELECT id, project_id, title, description, scopes, status, assignee_id, created_at, updated_at
FROM tasks WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.Task
	for rows.Next() {
		var t task.Task
		var scopes []byte
		var status string
		var assignee sql.NullString
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &scopes,
			&status, &assignee, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = task.Status(status)
		if assignee.Valid {
			t.AssigneeID = assignee.String
		}
		if err := parseList(scopes, &t.Scopes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PGRepo) SaveWorkspace(ctx context.Context, w workspace.Workspace) error {
	_, err := p.db.ExecContext(ctx, `
INSERT INTO workspaces(id, project_id, task_id, agent_id, runner_id, repo_path, base_commit, path, status, created_at, updated_at, last_exit, last_stdout, last_stderr, last_test_exit)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (id) DO UPDATE SET agent_id=EXCLUDED.agent_id, runner_id=EXCLUDED.runner_id,
status=EXCLUDED.status, updated_at=EXCLUDED.updated_at, last_exit=EXCLUDED.last_exit,
last_stdout=EXCLUDED.last_stdout, last_stderr=EXCLUDED.last_stderr, last_test_exit=EXCLUDED.last_test_exit`,
	w.ID, w.ProjectID, w.TaskID, nullUUID(w.AgentID), nullUUID(w.RunnerID),
	w.RepoPath, w.Base, w.Path, string(w.Status), w.CreatedAt, w.UpdatedAt,
	w.LastExit, w.LastStdout, w.LastStderr, w.LastTest)
	return err
}

func (p *PGRepo) DeleteWorkspace(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("unknown workspace %s", id)
	}
	return nil
}

func scanWorkspace(row *sql.Row) (workspace.Workspace, error) {
	var w workspace.Workspace
	var status string
	var agent, runner sql.NullString
	err := row.Scan(&w.ID, &w.ProjectID, &w.TaskID, &agent, &runner,
		&w.RepoPath, &w.Base, &w.Path, &status, &w.CreatedAt, &w.UpdatedAt,
		&w.LastExit, &w.LastStdout, &w.LastStderr, &w.LastTest)
	if err != nil {
		return workspace.Workspace{}, err
	}
	w.Status = workspace.Status(status)
	if agent.Valid {
		w.AgentID = agent.String
	}
	if runner.Valid {
		w.RunnerID = runner.String
	}
	return w, nil
}

func (p *PGRepo) GetWorkspace(ctx context.Context, id string) (workspace.Workspace, error) {
	w, err := scanWorkspace(p.db.QueryRowContext(ctx, `
SELECT id, project_id, task_id, agent_id, runner_id, repo_path, base_commit, path, status, created_at, updated_at, last_exit, last_stdout, last_stderr, last_test_exit
FROM workspaces WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return workspace.Workspace{}, fmt.Errorf("unknown workspace %s", id)
	}
	return w, err
}

func (p *PGRepo) ListWorkspaces(ctx context.Context, projectID string) ([]workspace.Workspace, error) {
	rows, err := p.db.QueryContext(ctx, `
SELECT id, project_id, task_id, agent_id, runner_id, repo_path, base_commit, path, status, created_at, updated_at, last_exit, last_stdout, last_stderr, last_test_exit
FROM workspaces WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []workspace.Workspace
	for rows.Next() {
		var w workspace.Workspace
		var status string
		var agent, runner sql.NullString
		if err := rows.Scan(&w.ID, &w.ProjectID, &w.TaskID, &agent, &runner,
			&w.RepoPath, &w.Base, &w.Path, &status, &w.CreatedAt, &w.UpdatedAt,
			&w.LastExit, &w.LastStdout, &w.LastStderr, &w.LastTest); err != nil {
			return nil, err
		}
		w.Status = workspace.Status(status)
		if agent.Valid {
			w.AgentID = agent.String
		}
		if runner.Valid {
			w.RunnerID = runner.String
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *PGRepo) SaveChangeset(ctx context.Context, c changeset.Changeset) error {
	_, err := p.db.ExecContext(ctx, `
INSERT INTO changesets(id, project_id, task_id, agent_id, base_commit, files, diff, test_ref, status, created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO UPDATE SET files=EXCLUDED.files, diff=EXCLUDED.diff,
test_ref=EXCLUDED.test_ref, status=EXCLUDED.status`,
		c.ID, c.ProjectID, c.TaskID, nullUUID(c.AgentID), c.Base,
		strList(c.Files), c.Diff, c.TestRef, string(c.Status), c.CreatedAt)
	return err
}

func (p *PGRepo) GetChangeset(ctx context.Context, id string) (changeset.Changeset, error) {
	var c changeset.Changeset
	var files []byte
	var status string
	var agent sql.NullString
	err := p.db.QueryRowContext(ctx, `
SELECT id, project_id, task_id, agent_id, base_commit, files, diff, test_ref, status, created_at
FROM changesets WHERE id=$1`, id).Scan(
		&c.ID, &c.ProjectID, &c.TaskID, &agent, &c.Base, &files,
		&c.Diff, &c.TestRef, &status, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return changeset.Changeset{}, fmt.Errorf("unknown changeset %s", id)
	}
	if err != nil {
		return changeset.Changeset{}, err
	}
	c.Status = changeset.Status(status)
	if agent.Valid {
		c.AgentID = agent.String
	}
	if err := parseList(files, &c.Files); err != nil {
		return changeset.Changeset{}, err
	}
	return c, nil
}

func (p *PGRepo) ListChangesets(ctx context.Context, projectID string) ([]changeset.Changeset, error) {
	rows, err := p.db.QueryContext(ctx, `
SELECT id, project_id, task_id, agent_id, base_commit, files, diff, test_ref, status, created_at
FROM changesets WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []changeset.Changeset
	for rows.Next() {
		var c changeset.Changeset
		var files []byte
		var status string
		var agent sql.NullString
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.TaskID, &agent, &c.Base, &files,
			&c.Diff, &c.TestRef, &status, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Status = changeset.Status(status)
		if agent.Valid {
			c.AgentID = agent.String
		}
		if err := parseList(files, &c.Files); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PGEventStore persists events; memory store remains the no-DB default.
type PGEventStore struct {
	db *sql.DB
}

func NewPGEventStore(db *sql.DB) *PGEventStore { return &PGEventStore{db: db} }

func (s *PGEventStore) Append(ctx context.Context, e events.Event) error {
	meta := "{}"
	if e.Metadata != nil {
		b, _ := json.Marshal(e.Metadata)
		meta = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events(id, project_id, actor_type, actor_id, type, entity_id, at, metadata)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, nullUUID(e.ProjectID), string(e.ActorType), e.ActorID,
		string(e.Type), e.EntityID, e.At, meta)
	return err
}

func (s *PGEventStore) List(ctx context.Context, projectID string, limit int) ([]events.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, project_id, actor_type, actor_id, type, entity_id, at, metadata
FROM events WHERE ($1='' OR project_id=NULLIF($1,'')::uuid) ORDER BY at DESC LIMIT $2`,
		projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []events.Event
	for rows.Next() {
		var e events.Event
		var proj sql.NullString
		var actor, typ string
		var meta []byte
		if err := rows.Scan(&e.ID, &proj, &actor, &e.ActorID, &typ, &e.EntityID, &e.At, &meta); err != nil {
			return nil, err
		}
		if proj.Valid {
			e.ProjectID = proj.String
		}
		e.ActorType = events.ActorType(actor)
		e.Type = events.Type(typ)
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Metadata)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *PGRepo) ListProjects(ctx context.Context) ([]project.Project, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT id, org_id, name, repo_path, branch, canonical_sha, created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []project.Project{}
	for rows.Next() {
		var pr project.Project
		if err := rows.Scan(&pr.ID, &pr.OrgID, &pr.Name, &pr.RepoPath, &pr.Branch, &pr.CanonicalSHA, &pr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}
