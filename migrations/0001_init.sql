-- Ballast MVP schema: coordination truth. Execution truth stays in Git.
-- Every table carries IDs + timestamps for auditability. Semantic-conflict
-- columns (symbols, contracts) arrive later without touching these.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS organizations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES organizations(id),
  email TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES organizations(id),
  name TEXT NOT NULL,
  repo_path TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT 'main',
  canonical_sha TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_projects_org ON projects(org_id);

CREATE TABLE IF NOT EXISTS runners (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  hostname TEXT NOT NULL,
  os TEXT NOT NULL DEFAULT '',
  arch TEXT NOT NULL DEFAULT '',
  cpu INT NOT NULL DEFAULT 0,
  memory_mb INT NOT NULL DEFAULT 0,
  has_docker BOOL NOT NULL DEFAULT FALSE,
  has_git BOOL NOT NULL DEFAULT TRUE,
  online BOOL NOT NULL DEFAULT TRUE,
  capabilities JSONB NOT NULL DEFAULT '[]',
  last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_instances (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  provider TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  active BOOL NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  scopes JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'TODO',
  assignee_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);

CREATE TABLE IF NOT EXISTS workspaces (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  task_id UUID NOT NULL REFERENCES tasks(id),
  agent_id UUID REFERENCES agent_instances(id),
  runner_id UUID REFERENCES runners(id),
  repo_path TEXT NOT NULL,
  base_commit TEXT NOT NULL,
  path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'CREATING',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_exit INT NOT NULL DEFAULT 0,
  last_stdout TEXT NOT NULL DEFAULT '',
  last_stderr TEXT NOT NULL DEFAULT '',
  last_test_exit INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_workspaces_project ON workspaces(project_id);

CREATE TABLE IF NOT EXISTS leases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  owner_id UUID NOT NULL,
  pattern TEXT NOT NULL,
  active BOOL NOT NULL DEFAULT TRUE,
  acquired_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_leases_project ON leases(project_id);

CREATE TABLE IF NOT EXISTS changesets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  task_id UUID NOT NULL REFERENCES tasks(id),
  agent_id UUID REFERENCES agent_instances(id),
  base_commit TEXT NOT NULL,
  files JSONB NOT NULL DEFAULT '[]',
  diff TEXT NOT NULL DEFAULT '',
  test_ref TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'DRAFT',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_changesets_project ON changesets(project_id);

CREATE TABLE IF NOT EXISTS conflicts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id),
  workspace_a UUID NOT NULL,
  workspace_b UUID,
  kind TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT 'WARNING',
  affected_files JSONB NOT NULL DEFAULT '[]',
  reason TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'OPEN',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_conflicts_project ON conflicts(project_id);

CREATE TABLE IF NOT EXISTS events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID,
  actor_type TEXT NOT NULL,
  actor_id TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL,
  entity_id TEXT NOT NULL DEFAULT '',
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  metadata JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, at);

CREATE TABLE IF NOT EXISTS audit_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID,
  actor_type TEXT NOT NULL,
  actor_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  entity_id TEXT NOT NULL DEFAULT '',
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  metadata JSONB NOT NULL DEFAULT '{}'
);
