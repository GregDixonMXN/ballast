// Package project owns organizations, users, projects, repositories,
// runners, and agent instances — the coordination-truth records.
// Execution truth stays in Git; this package tracks who/what/where.
package project

import (
	"time"

	"github.com/google/uuid"
)

// Organization scopes users, projects, and billing later.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User is a human collaborator (OIDC subject in prod, local id in dev).
type User struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Project groups a repository with its tasks, workspaces, and events.
type Project struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	Name         string    `json:"name"`
	RepoPath     string    `json:"repo_path"`
	Branch       string    `json:"branch"`
	CanonicalSHA string    `json:"canonical_sha"`
	CreatedAt    time.Time `json:"created_at"`
}

// RunnerNode is an execution machine (local or remote).
type RunnerNode struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	CPU          int       `json:"cpu"`
	MemoryMB     int       `json:"memory_mb"`
	HasDocker    bool      `json:"has_docker"`
	HasGit       bool      `json:"has_git"`
	Online       bool      `json:"online"`
	LastSeen     time.Time `json:"last_seen"`
	Capabilities []string  `json:"capabilities,omitempty"`
}

// AgentProvider names a supported worker backend.
type AgentProvider string

const (
	ProviderCodex  AgentProvider = "codex"
	ProviderClaude AgentProvider = "claude"
	ProviderShell  AgentProvider = "shell"
	ProviderCustom AgentProvider = "custom"
)

// AgentInstance is one worker (human session or agent process).
type AgentInstance struct {
	ID        string        `json:"id"`
	ProjectID string        `json:"project_id"`
	Provider  AgentProvider `json:"provider"`
	Label     string        `json:"label"`
	Active    bool          `json:"active"`
	CreatedAt time.Time     `json:"created_at"`
}

func newID() string { return uuid.NewString() }

// NewProject validates and builds a project record.
func NewProject(orgID, name, repoPath, branch string) Project {
	return Project{ID: newID(), OrgID: orgID, Name: name, RepoPath: repoPath,
		Branch: branch, CreatedAt: time.Now().UTC()}
}
