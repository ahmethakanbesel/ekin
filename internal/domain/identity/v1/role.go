// Package identity models who may do what: users, their sessions, their
// memberships in workspaces and the invitations that create memberships.
package identity

import "kurgu/internal/apperror"

// Role is a member's role in one workspace.
type Role string

const (
	// RoleOwner does everything, including settings and members.
	RoleOwner Role = "owner"
	// RoleEditor runs and edits pipelines and reviews stories.
	RoleEditor Role = "editor"
	// RoleViewer reads.
	RoleViewer Role = "viewer"
)

// ParseRole parses a wire role.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleOwner, RoleEditor, RoleViewer:
		return r, nil
	}
	return "", apperror.New(apperror.ValidationFailed, "role must be owner, editor or viewer")
}

// Action is a class of operation inside a workspace. Every API operation
// on a workspace maps to exactly one.
type Action int

const (
	// ActionView reads anything in the workspace.
	ActionView Action = iota + 1
	// ActionEdit changes content: pipelines, runs, jobs, reviews.
	ActionEdit
	// ActionManage changes the workspace itself: settings, members,
	// invitations.
	ActionManage
)

func (a Action) String() string {
	switch a {
	case ActionView:
		return "view"
	case ActionEdit:
		return "edit"
	case ActionManage:
		return "manage"
	}
	return "unknown"
}

// Can reports whether r allows a.
func (r Role) Can(a Action) bool {
	switch r {
	case RoleOwner:
		return a == ActionView || a == ActionEdit || a == ActionManage
	case RoleEditor:
		return a == ActionView || a == ActionEdit
	case RoleViewer:
		return a == ActionView
	}
	return false
}
