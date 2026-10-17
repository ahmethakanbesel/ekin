// Package handler implements the media library operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	librarydomain "ekin/internal/domain/library/v1"
	storydomain "ekin/internal/domain/story/v1"
	"ekin/internal/gen"
	librarysvc "ekin/internal/service/library/v1"
	productionsvc "ekin/internal/service/production/v1"
	"ekin/internal/transport/http/principal"
)

// maxDetailsBytes bounds the details part of an upload.
const maxDetailsBytes = 16 << 10

// Stories names the stories that used an asset, and lists and saves the
// media a video revision uses (the production service).
type Stories interface {
	Get(ctx context.Context, workspaceID, storyID uuid.UUID) (*storydomain.Story, error)
	Media(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, number int) ([]productionsvc.RevisionMedia, error)
	SaveMedia(ctx context.Context, workspaceID, storyID, deliverableID uuid.UUID, number int, key string, actorID uuid.UUID) (*librarydomain.Asset, error)
}

// Handler serves the library.
type Handler struct {
	svc     *librarysvc.Service
	stories Stories
}

// New returns a Handler. stories may be nil: usages then carry no titles.
func New(svc *librarysvc.Service, stories Stories) *Handler {
	return &Handler{svc: svc, stories: stories}
}

// wire converts assets, with their usage counts.
func (h *Handler) wire(ctx context.Context, workspaceID uuid.UUID, assets ...*librarydomain.Asset) ([]gen.LibraryAsset, error) {
	ids := make([]uuid.UUID, len(assets))
	for i, a := range assets {
		ids[i] = a.ID()
	}
	counts, err := h.svc.UsageCounts(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LibraryAsset, len(assets))
	for i, a := range assets {
		out[i] = ToWire(a, counts[a.ID()])
	}
	return out, nil
}

// one converts a single asset.
func (h *Handler) one(ctx context.Context, workspaceID uuid.UUID, a *librarydomain.Asset) (gen.LibraryAsset, error) {
	out, err := h.wire(ctx, workspaceID, a)
	if err != nil {
		return gen.LibraryAsset{}, err
	}
	return out[0], nil
}

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
	out, err := h.wire(ctx, req.WorkspaceId, assets...)
	if err != nil {
		return nil, err
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
			// A new asset has no usages.
			return gen.UploadLibraryAsset202JSONResponse(ToWire(a, 0)), nil
		}
		_ = part.Close()
	}
}

func (h *Handler) GetLibraryAsset(ctx context.Context, req gen.GetLibraryAssetRequestObject) (gen.GetLibraryAssetResponseObject, error) {
	a, err := h.svc.Get(ctx, req.WorkspaceId, req.AssetId)
	if err != nil {
		return nil, err
	}
	out, err := h.one(ctx, req.WorkspaceId, a)
	if err != nil {
		return nil, err
	}
	return gen.GetLibraryAsset200JSONResponse(out), nil
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
	out, err := h.one(ctx, req.WorkspaceId, a)
	if err != nil {
		return nil, err
	}
	return gen.UpdateLibraryAsset200JSONResponse(out), nil
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
	out, err := h.one(ctx, req.WorkspaceId, a)
	if err != nil {
		return nil, err
	}
	return gen.RetryLibraryAsset202JSONResponse(out), nil
}

func (h *Handler) GetLibrarySettings(ctx context.Context, req gen.GetLibrarySettingsRequestObject) (gen.GetLibrarySettingsResponseObject, error) {
	c, err := h.svc.Collect(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	return gen.GetLibrarySettings200JSONResponse{Music: c.Music, Video: c.Video, Photo: c.Photo}, nil
}

func (h *Handler) SaveLibrarySettings(ctx context.Context, req gen.SaveLibrarySettingsRequestObject) (gen.SaveLibrarySettingsResponseObject, error) {
	c, err := h.svc.SaveCollect(ctx, req.WorkspaceId, librarydomain.Collect{Music: req.Body.Music, Video: req.Body.Video, Photo: req.Body.Photo})
	if err != nil {
		return nil, err
	}
	return gen.SaveLibrarySettings200JSONResponse{Music: c.Music, Video: c.Video, Photo: c.Photo}, nil
}

func (h *Handler) ListRevisionMedia(ctx context.Context, req gen.ListRevisionMediaRequestObject) (gen.ListRevisionMediaResponseObject, error) {
	if h.stories == nil {
		return nil, apperror.New(apperror.NotFound, "no such endpoint")
	}
	media, err := h.stories.Media(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, req.Number)
	if err != nil {
		return nil, err
	}
	var assets []*librarydomain.Asset
	for _, m := range media {
		if m.Asset != nil {
			assets = append(assets, m.Asset)
		}
	}
	wired, err := h.wire(ctx, req.WorkspaceId, assets...)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]gen.LibraryAsset{}
	for _, a := range wired {
		byID[a.Id] = a
	}
	out := make([]gen.RevisionMedia, 0, len(media))
	for _, m := range media {
		item := gen.RevisionMedia{Key: m.Key, Kind: gen.LibraryAssetKind(m.Kind), Source: gen.RevisionMediaSource(m.Source), Title: m.Title}
		if m.Author != "" {
			author := m.Author
			item.Author = &author
		}
		if m.Asset != nil {
			a := byID[m.Asset.ID()]
			item.Asset = &a
		}
		out = append(out, item)
	}
	return gen.ListRevisionMedia200JSONResponse{Media: out}, nil
}

func (h *Handler) SaveRevisionMedia(ctx context.Context, req gen.SaveRevisionMediaRequestObject) (gen.SaveRevisionMediaResponseObject, error) {
	if h.stories == nil {
		return nil, apperror.New(apperror.NotFound, "no such endpoint")
	}
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	a, err := h.stories.SaveMedia(ctx, req.WorkspaceId, req.StoryId, req.DeliverableId, req.Number, req.Body.Key, p.User.ID())
	if err != nil {
		return nil, err
	}
	out, err := h.one(ctx, req.WorkspaceId, a)
	if err != nil {
		return nil, err
	}
	return gen.SaveRevisionMedia200JSONResponse(out), nil
}

// maxUsages bounds the usages listed for one asset.
const maxUsages = 100

func (h *Handler) ListLibraryAssetUsages(ctx context.Context, req gen.ListLibraryAssetUsagesRequestObject) (gen.ListLibraryAssetUsagesResponseObject, error) {
	uses, err := h.svc.Usages(ctx, req.WorkspaceId, req.AssetId)
	if err != nil {
		return nil, err
	}
	if len(uses) > maxUsages {
		uses = uses[:maxUsages]
	}
	titles := map[uuid.UUID]*string{}
	out := make([]gen.LibraryUsage, 0, len(uses))
	for _, u := range uses {
		title, seen := titles[u.StoryID]
		if !seen && h.stories != nil {
			if st, err := h.stories.Get(ctx, req.WorkspaceId, u.StoryID); err == nil {
				t := st.Idea().Title
				title = &t
			} else if !apperror.Is(err, apperror.StoryNotFound) {
				return nil, err
			}
			titles[u.StoryID] = title
		}
		out = append(out, gen.LibraryUsage{StoryId: u.StoryID, StoryTitle: title, DeliverableId: u.DeliverableID,
			Revision: u.Revision, UsedAt: u.At})
	}
	return gen.ListLibraryAssetUsages200JSONResponse{Usages: out}, nil
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

// ToWire converts an asset to its API form; uses is how many videos used
// it.
func ToWire(a *librarydomain.Asset, uses int) gen.LibraryAsset {
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
		UsageCount:  uses,
		Provisional: a.Provisional(),
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
	if u := a.Harvest().SourceURL; u != "" {
		out.SourceUrl = &u
	}
	if u := a.Harvest().OriginalURL; u != "" {
		out.OriginalUrl = &u
	}
	if at := a.ArchivedAt(); !at.IsZero() {
		out.ArchivedAt = &at
	}
	return out
}
