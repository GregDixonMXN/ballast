-- Report evidence on workspaces: persisted runner outcome per run.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS last_exit INT NOT NULL DEFAULT 0;
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS last_stdout TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS last_stderr TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS last_test_exit INT NOT NULL DEFAULT 0;
