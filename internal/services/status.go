package services

import (
	"context"

	"ballast/internal/changeset"
)

// SetHead records a moved canonical head. Only integration calls this.
func (s *Projects) SetHead(id, sha string) error {
	return s.Repo.SetCanonicalHead(context.Background(), id, sha)
}

// List returns a project's changesets oldest-first for revalidation.
func (s *Changesets) List(projectID string) ([]changeset.Changeset, error) {
	return s.Repo.ListChangesets(context.Background(), projectID)
}

// Mark sets a changeset status directly. Review decisions go through
// Decide; integration outcomes (MERGED/NEEDS_REBASE/CONFLICTED) go here.
func (s *Changesets) Mark(id string, to changeset.Status) (changeset.Changeset, error) {
	ctx := context.Background()
	cs, err := s.Repo.GetChangeset(ctx, id)
	if err != nil {
		return changeset.Changeset{}, err
	}
	cs.Status = to
	if err := s.Repo.SaveChangeset(ctx, cs); err != nil {
		return changeset.Changeset{}, err
	}
	return cs, nil
}
