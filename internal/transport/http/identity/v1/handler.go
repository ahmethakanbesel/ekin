// Package handler implements the auth, account, member and invitation
// operations of gen.StrictServerInterface.
package handler

import (
	"context"
	"strings"

	identitydomain "kurgu/internal/domain/identity/v1"
	"kurgu/internal/gen"
	identitysvc "kurgu/internal/service/identity/v1"
	"kurgu/internal/transport/http/principal"
)

// Handler serves identity operations.
type Handler struct {
	svc       *identitysvc.Service
	cookies   principal.Cookies
	publicURL string
}

// New returns a Handler. publicURL prefixes invitation links.
func New(svc *identitysvc.Service, cookies principal.Cookies, publicURL string) *Handler {
	return &Handler{svc: svc, cookies: cookies, publicURL: strings.TrimRight(publicURL, "/")}
}

func (h *Handler) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	email, err := identitydomain.ParseEmail(req.Body.Email)
	if err != nil {
		return nil, err
	}
	in, err := h.svc.Login(ctx, email, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return gen.Login200JSONResponse{
		Body:    userToWire(in.User),
		Headers: gen.Login200ResponseHeaders{SetCookie: h.cookies.Session(in.Token, in.Session)},
	}, nil
}

func (h *Handler) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Logout(ctx, p.Session.ID()); err != nil {
		return nil, err
	}
	return gen.Logout204Response{Headers: gen.Logout204ResponseHeaders{SetCookie: h.cookies.Clear()}}, nil
}

func (h *Handler) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetMe200JSONResponse(userToWire(p.User)), nil
}

func (h *Handler) UpdateMe(ctx context.Context, req gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.svc.Rename(ctx, p.User.ID(), req.Body.Name)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(userToWire(u)), nil
}

func (h *Handler) ChangePassword(ctx context.Context, req gen.ChangePasswordRequestObject) (gen.ChangePasswordResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	next, err := identitydomain.ParsePassword(req.Body.NewPassword)
	if err != nil {
		return nil, err
	}
	if err := h.svc.ChangePassword(ctx, p.User.ID(), p.Session.ID(), req.Body.CurrentPassword, next); err != nil {
		return nil, err
	}
	return gen.ChangePassword204Response{}, nil
}

func (h *Handler) GetInvitation(ctx context.Context, req gen.GetInvitationRequestObject) (gen.GetInvitationResponseObject, error) {
	preview, err := h.svc.PreviewInvitation(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	inv := preview.Invitation
	return gen.GetInvitation200JSONResponse{
		WorkspaceName: preview.WorkspaceName,
		Email:         string(inv.Email()),
		Role:          gen.Role(inv.Role()),
		ExpiresAt:     inv.ExpiresAt(),
		AccountExists: preview.AccountExists,
	}, nil
}

func (h *Handler) AcceptInvitation(ctx context.Context, req gen.AcceptInvitationRequestObject) (gen.AcceptInvitationResponseObject, error) {
	var name string
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	got, err := h.svc.AcceptInvitation(ctx, req.Token, name, req.Body.Password)
	if err != nil {
		return nil, err
	}
	resp := gen.AcceptInvitation200JSONResponse{
		Headers: gen.AcceptInvitation200ResponseHeaders{SetCookie: h.cookies.Session(got.Token, got.Session)},
	}
	resp.Body.User = userToWire(got.User)
	resp.Body.WorkspaceId = got.WorkspaceID
	return resp, nil
}

func (h *Handler) ListMembers(ctx context.Context, req gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	members, err := h.svc.Members(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Member, 0, len(members))
	for _, m := range members {
		out = append(out, memberToWire(m))
	}
	return gen.ListMembers200JSONResponse{Members: out}, nil
}

func (h *Handler) ChangeMemberRole(ctx context.Context, req gen.ChangeMemberRoleRequestObject) (gen.ChangeMemberRoleResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	role, err := identitydomain.ParseRole(string(req.Body.Role))
	if err != nil {
		return nil, err
	}
	m, err := h.svc.ChangeRole(ctx, req.WorkspaceId, p.User.ID(), req.UserId, role)
	if err != nil {
		return nil, err
	}
	return gen.ChangeMemberRole200JSONResponse(memberToWire(m)), nil
}

func (h *Handler) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.RemoveMember(ctx, req.WorkspaceId, p.User.ID(), req.UserId); err != nil {
		return nil, err
	}
	return gen.RemoveMember204Response{}, nil
}

func (h *Handler) ListInvitations(ctx context.Context, req gen.ListInvitationsRequestObject) (gen.ListInvitationsResponseObject, error) {
	invs, err := h.svc.Invitations(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Invitation, 0, len(invs))
	for _, inv := range invs {
		out = append(out, invitationToWire(inv))
	}
	return gen.ListInvitations200JSONResponse{Invitations: out}, nil
}

func (h *Handler) CreateInvitation(ctx context.Context, req gen.CreateInvitationRequestObject) (gen.CreateInvitationResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	email, err := identitydomain.ParseEmail(req.Body.Email)
	if err != nil {
		return nil, err
	}
	role, err := identitydomain.ParseRole(string(req.Body.Role))
	if err != nil {
		return nil, err
	}
	inv, token, err := h.svc.Invite(ctx, req.WorkspaceId, p.User.ID(), email, role)
	if err != nil {
		return nil, err
	}
	return gen.CreateInvitation201JSONResponse{
		Invitation: invitationToWire(inv),
		Url:        h.publicURL + "/invite/" + token,
	}, nil
}

func (h *Handler) RevokeInvitation(ctx context.Context, req gen.RevokeInvitationRequestObject) (gen.RevokeInvitationResponseObject, error) {
	if err := h.svc.RevokeInvitation(ctx, req.WorkspaceId, req.InvitationId); err != nil {
		return nil, err
	}
	return gen.RevokeInvitation204Response{}, nil
}

func userToWire(u *identitydomain.User) gen.User {
	return gen.User{Id: u.ID(), Email: string(u.Email()), Name: u.Name(), CreatedAt: u.CreatedAt()}
}

func memberToWire(m identitysvc.Member) gen.Member {
	return gen.Member{
		UserId:   m.User.ID(),
		Email:    string(m.User.Email()),
		Name:     m.User.Name(),
		Role:     gen.Role(m.Membership.Role()),
		JoinedAt: m.Membership.CreatedAt(),
	}
}

func invitationToWire(inv *identitydomain.Invitation) gen.Invitation {
	return gen.Invitation{
		Id:        inv.ID(),
		Email:     string(inv.Email()),
		Role:      gen.Role(inv.Role()),
		CreatedAt: inv.CreatedAt(),
		ExpiresAt: inv.ExpiresAt(),
	}
}
