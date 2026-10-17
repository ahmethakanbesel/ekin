package library

import (
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/pkg/timeutil"
)

// Usage records that a revision of a deliverable used an asset. Reuse is
// unlimited; usages are only counted.
type Usage struct {
	AssetID       uuid.UUID
	WorkspaceID   uuid.UUID
	StoryID       uuid.UUID
	DeliverableID uuid.UUID
	Revision      int
	At            time.Time
}

// NewUsage builds a usage.
func NewUsage(assetID, workspaceID, storyID, deliverableID uuid.UUID, revision int, at time.Time) (Usage, error) {
	if assetID == uuid.Nil || workspaceID == uuid.Nil || storyID == uuid.Nil || deliverableID == uuid.Nil {
		return Usage{}, apperror.New(apperror.ValidationFailed, "a usage needs its asset, workspace, story and deliverable")
	}
	if revision < 1 {
		return Usage{}, apperror.New(apperror.ValidationFailed, "a usage needs a revision")
	}
	return Usage{AssetID: assetID, WorkspaceID: workspaceID, StoryID: storyID, DeliverableID: deliverableID,
		Revision: revision, At: timeutil.Normalize(at)}, nil
}
