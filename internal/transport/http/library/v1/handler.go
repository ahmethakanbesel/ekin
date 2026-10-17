// Package handler implements the media library operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"kurgu/internal/apperror"
	librarydomain "kurgu/internal/domain/library/v1"
	"kurgu/internal/gen"
	librarysvc "kurgu/internal/service/library/v1"
	"kurgu/internal/transport/http/principal"
)

// maxDetailsBytes bounds the details part of an upload.
const maxDetailsBytes = 16 << 10

// Handler serves the library.
type Handler struct {
	svc *librarysvc.Service
}

// New returns a Handler.
func New(svc *librarysvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListLibraryAssets(ctx context.Context, req gen.ListLibraryAssetsRequestObject) (gen.ListLibraryAssetsResponseObject, error) {
	var f librarysvc.Filter
	var err error
	if f.Kinds, err = parseAll(req.Params.Kind, librarydomain.ParseKind); err != nil {
		return nil, err
	}
	if f.Origins, err = parseAll(req.Params.Origin, librarydomain.ParseOrigin); err != nil {
		return nil, err
	}
	if f.Statuses, err = parseAll(req.Params.Status, librarydomain.ParseStatus); err != nil {
		return nil, err
	}
	if f.Orientations, err = parseAll(req.Params.Orientation, librarydomain.ParseOrientation); err != nil {
		return nil, err
	}
	if req.Params.Q != nil {
		f.Query = *req.Params.Q
	}
	if req.Params.Archived != nil {
		f.Archived = *req.Params.Archived
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		f.After = *req.Params.Cursor
	}
	assets, next, err := h.svc.List(ctx, req.WorkspaceId, f)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LibraryAsset, 0, len(assets))
	for _, a := range assets {
		out = append(out, ToWire(a))
	}
	resp := gen.ListLibraryAssets200JSONResponse{Assets: out}
	if next != "" {
		resp.NextCursor = &next
	}
	return resp, nil
}

func parseAll[W ~string, D any](raw *[]W, parse func(string) (D, error)) ([]D, error) {
	if raw == nil {
		return nil, nil
	}
	out := make([]D, 0, len(*raw))
	for _, r := range *raw {
		v, err := parse(string(r))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// UploadLibraryAsset streams the "file" part straight into the service;
// the optional "kind" and "details" parts must come before it.
func (h *Handler) UploadLibraryAsset(ctx context.Context, req gen.UploadLibraryAssetRequestObject) (gen.UploadLibraryAssetResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	var (
		kind    librarydomain.Kind
		details librarydomain.Details
	)
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, apperror.New(apperror.ValidationFailed, "the upload has no file")
		}
		if err != nil {
			return nil, apperror.Wrap(apperror.BadRequest, err, "malformed upload")
		}
		switch part.FormName() {
		case "kind":
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			if raw := strings.TrimSpace(string(b)); raw != "" {
				if kind, err = librarydomain.ParseKind(raw); err != nil {
					_ = part.Close()
					return nil, err
				}
			}
		case "details":
			var d gen.LibraryAssetDetails
			if err := json.NewDecoder(io.LimitReader(part, maxDetailsBytes)).Decode(&d); err != nil {
				_ = part.Close()
				return nil, apperror.Wrap(apperror.BadRequest, err, "details must be JSON")
			}
			details = FromWire(d)
		case "file":
			a, err := h.svc.Upload(ctx, req.WorkspaceId, p.User.ID(), kind, details, part.FileName(), part)
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			return gen.UploadLibraryAsset202JSONResponse(ToWire(a)), nil
		}
		_ = part.Close()
	}
}

func (h *Handler) GetLibraryAsset(ctx context.Context, req gen.GetLibraryAssetRequestObject) (gen.GetLibraryAssetResponseObject, error) {
	a, err := h.svc.Get(ctx, req.WorkspaceId, req.AssetId)
	if err != nil {
		return nil, err
	}
	return gen.GetLibraryAsset200JSONResponse(ToWire(a)), nil
}

func (h *Handler) UpdateLibraryAsset(ctx context.Context, req gen.UpdateLibraryAssetRequestObject) (gen.UpdateLibraryAssetResponseObject, error) {
	var patch librarysvc.Patch
	if req.Body.Details != nil {
		d := FromWire(*req.Body.Details)
		patch.Details = &d
	}
	patch.Archived = req.Body.Archived
	a, err := h.svc.Update(ctx, req.WorkspaceId, req.AssetId, patch)
	if err != nil {
		return nil, err
	}
	return gen.UpdateLibraryAsset200JSONResponse(ToWire(a)), nil
}

func (h *Handler) DeleteLibraryAsset(ctx context.Context, req gen.DeleteLibraryAssetRequestObject) (gen.DeleteLibraryAssetResponseObject, error) {
	if err := h.svc.Delete(ctx, req.WorkspaceId, req.AssetId); err != nil {
		return nil, err
	}
	return gen.DeleteLibraryAsset204Response{}, nil
}

func (h *Handler) RetryLibraryAsset(ctx context.Context, req gen.RetryLibraryAssetRequestObject) (gen.RetryLibraryAssetResponseObject, error) {
	a, err := h.svc.Retry(ctx, req.WorkspaceId, req.AssetId)
	if err != nil {
		return nil, err
	}
	return gen.RetryLibraryAsset202JSONResponse(ToWire(a)), nil
}

// FromWire converts wire details to domain details.
func FromWire(d gen.LibraryAssetDetails) librarydomain.Details {
	out := librarydomain.Details{Title: d.Title}
	if d.Description != nil {
		out.Description = *d.Description
	}
	if d.Tags != nil {
		out.Tags = *d.Tags
	}
	if d.Mood != nil {
		out.Mood = *d.Mood
	}
	if d.Tempo != nil {
		out.Tempo = *d.Tempo
	}
	if d.Credit != nil {
		out.Credit = librarydomain.Credit{Author: d.Credit.Author, URL: d.Credit.Url, Licence: d.Credit.Licence}
	}
	return out
}

// ToWire converts an asset to its API form.
func ToWire(a *librarydomain.Asset) gen.LibraryAsset {
	c := a.Credit()
	out := gen.LibraryAsset{
		Id:          a.ID(),
		Kind:        gen.LibraryAssetKind(a.Kind()),
		Origin:      gen.LibraryAssetOrigin(a.Origin()),
		Status:      gen.LibraryAssetStatus(a.Status()),
		Title:       a.Title(),
		Description: a.Description(),
		Tags:        a.Tags(),
		Credit:      gen.LibraryCredit{Author: c.Author, Url: c.URL, Licence: c.Licence},
		Filename:    a.Filename(),
		Size:        a.Size(),
		HasPoster:   a.HasPoster(),
		CreatedAt:   a.CreatedAt(),
		UpdatedAt:   a.UpdatedAt(),
	}
	if a.Kind() == librarydomain.KindMusic {
		mood, tempo := a.Mood(), a.Tempo()
		out.Mood, out.Tempo = &mood, &tempo
	}
	if a.Width() > 0 {
		w, h, o := a.Width(), a.Height(), gen.Orientation(a.Orientation())
		out.Width, out.Height, out.Orientation = &w, &h, &o
	}
	if d := a.Duration(); d > 0 {
		secs := float32(d.Seconds())
		out.DurationSeconds = &secs
	}
	if f := a.Failure(); f != "" {
		out.Failure = &f
	}
	if at := a.ArchivedAt(); !at.IsZero() {
		out.ArchivedAt = &at
	}
	return out
}
