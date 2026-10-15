// Package handler implements the knowledge-base operations of
// gen.StrictServerInterface.
package handler

import (
	"context"
	"errors"
	"io"
	"strings"

	"kurgu/internal/apperror"
	knowledgedomain "kurgu/internal/domain/knowledge/v1"
	"kurgu/internal/gen"
	knowledgesvc "kurgu/internal/service/knowledge/v1"
	"kurgu/internal/transport/http/principal"
)

// maxPagesPerCall bounds one pages listing.
const maxPagesPerCall = 200

// Handler serves documents.
type Handler struct {
	svc *knowledgesvc.Service
}

// New returns a Handler.
func New(svc *knowledgesvc.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListDocuments(ctx context.Context, req gen.ListDocumentsRequestObject) (gen.ListDocumentsResponseObject, error) {
	docs, err := h.svc.List(ctx, req.WorkspaceId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Document, 0, len(docs))
	for _, d := range docs {
		out = append(out, ToWire(d))
	}
	return gen.ListDocuments200JSONResponse{Documents: out}, nil
}

// UploadDocument streams the "file" part straight into the service; the
// optional "title" part must come before it.
func (h *Handler) UploadDocument(ctx context.Context, req gen.UploadDocumentRequestObject) (gen.UploadDocumentResponseObject, error) {
	p, err := principal.From(ctx)
	if err != nil {
		return nil, err
	}
	var title string
	for {
		part, err := req.Body.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, apperror.New(apperror.ValidationFailed, "the upload has no file")
		}
		if err != nil {
			return nil, apperror.Wrap(apperror.BadRequest, err, "malformed upload")
		}
		switch part.FormName() {
		case "title":
			b, _ := io.ReadAll(io.LimitReader(part, 1024))
			title = strings.TrimSpace(string(b))
		case "file":
			d, err := h.svc.Upload(ctx, req.WorkspaceId, p.User.ID(), title, part.FileName(), part)
			_ = part.Close()
			if err != nil {
				return nil, err
			}
			return gen.UploadDocument202JSONResponse(ToWire(d)), nil
		}
		_ = part.Close()
	}
}

func (h *Handler) GetDocument(ctx context.Context, req gen.GetDocumentRequestObject) (gen.GetDocumentResponseObject, error) {
	d, err := h.svc.Get(ctx, req.WorkspaceId, req.DocumentId)
	if err != nil {
		return nil, err
	}
	return gen.GetDocument200JSONResponse(ToWire(d)), nil
}

func (h *Handler) UpdateDocument(ctx context.Context, req gen.UpdateDocumentRequestObject) (gen.UpdateDocumentResponseObject, error) {
	d, err := h.svc.Rename(ctx, req.WorkspaceId, req.DocumentId, req.Body.Title)
	if err != nil {
		return nil, err
	}
	return gen.UpdateDocument200JSONResponse(ToWire(d)), nil
}

func (h *Handler) DeleteDocument(ctx context.Context, req gen.DeleteDocumentRequestObject) (gen.DeleteDocumentResponseObject, error) {
	if err := h.svc.Delete(ctx, req.WorkspaceId, req.DocumentId); err != nil {
		return nil, err
	}
	return gen.DeleteDocument204Response{}, nil
}

func (h *Handler) RetryDocument(ctx context.Context, req gen.RetryDocumentRequestObject) (gen.RetryDocumentResponseObject, error) {
	d, err := h.svc.Retry(ctx, req.WorkspaceId, req.DocumentId)
	if err != nil {
		return nil, err
	}
	return gen.RetryDocument202JSONResponse(ToWire(d)), nil
}

func (h *Handler) ListDocumentPages(ctx context.Context, req gen.ListDocumentPagesRequestObject) (gen.ListDocumentPagesResponseObject, error) {
	from, to := 1, 0
	if req.Params.From != nil {
		from = *req.Params.From
	}
	if req.Params.To != nil {
		to = *req.Params.To
	}
	if to != 0 && to < from {
		return nil, apperror.New(apperror.ValidationFailed, "to must not be before from")
	}
	if to == 0 || to-from+1 > maxPagesPerCall {
		to = from + maxPagesPerCall - 1
	}
	_, pages, err := h.svc.Pages(ctx, req.WorkspaceId, req.DocumentId, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]gen.DocumentPage, 0, len(pages))
	for _, p := range pages {
		out = append(out, gen.DocumentPage{Number: p.Number, Text: p.Text, Words: p.Words, Tokens: p.Tokens})
	}
	return gen.ListDocumentPages200JSONResponse{Pages: out}, nil
}

func (h *Handler) GetDocumentContents(ctx context.Context, req gen.GetDocumentContentsRequestObject) (gen.GetDocumentContentsResponseObject, error) {
	sections, err := h.svc.Contents(ctx, req.WorkspaceId, req.DocumentId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.DocumentSection, 0, len(sections))
	for _, sec := range sections {
		out = append(out, gen.DocumentSection{Level: sec.Level, Title: sec.Title, FromPage: sec.From, ToPage: sec.To})
	}
	return gen.GetDocumentContents200JSONResponse{Sections: out}, nil
}

func (h *Handler) GetDocumentFile(ctx context.Context, req gen.GetDocumentFileRequestObject) (gen.GetDocumentFileResponseObject, error) {
	d, rc, size, err := h.svc.Open(ctx, req.WorkspaceId, req.DocumentId)
	if err != nil {
		return nil, err
	}
	body := closingReader{rc}
	switch d.Format() {
	case knowledgedomain.FormatPDF:
		return gen.GetDocumentFile200ApplicationpdfResponse{Body: body, ContentLength: size}, nil
	case knowledgedomain.FormatEPUB:
		return gen.GetDocumentFile200ApplicationepubZipResponse{Body: body, ContentLength: size}, nil
	}
	return gen.GetDocumentFile200ApplicationoctetStreamResponse{Body: body, ContentLength: size}, nil
}

// closingReader closes the blob once the response has copied it all.
type closingReader struct{ rc io.ReadCloser }

func (c closingReader) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if err != nil {
		_ = c.rc.Close()
	}
	return n, err
}

// ToWire converts a document to its API form.
func ToWire(d *knowledgedomain.Document) gen.Document {
	out := gen.Document{
		Id:        d.ID(),
		Title:     d.Title(),
		Filename:  d.Filename(),
		Format:    gen.DocumentFormat(d.Format()),
		Size:      d.Size(),
		Status:    gen.DocumentStatus(d.Status()),
		PageCount: d.PageCount(),
		Tokens:    d.Tokens(),
		CreatedAt: d.CreatedAt(),
		UpdatedAt: d.UpdatedAt(),
	}
	if f := d.Failure(); f != "" {
		out.Failure = &f
	}
	return out
}
