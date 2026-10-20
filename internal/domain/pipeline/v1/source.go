package pipeline

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ahmethakanbesel/ekin/internal/apperror"
)

// SourceKind is where a pipeline gets its ideas and material.
type SourceKind string

const (
	// SourcePrompt means ideas come from the pipeline's own instructions,
	// which act like a system prompt.
	SourcePrompt SourceKind = "prompt"
	// SourceKnowledge means ideas and material come from referenced
	// knowledge-base pages; instructions are optional guidance.
	SourceKnowledge SourceKind = "knowledge"
	// SourceSEO means each idea targets one open keyword opportunity from
	// the workspace's pool (OpenSEO); instructions set the brief.
	SourceSEO SourceKind = "seo"
)

// Source limits.
const (
	MaxInstructionsLength = 20000
	MaxKnowledgeRefs      = 50
)

// PageRange is an inclusive 1-based page range. The zero value means the
// whole document.
type PageRange struct {
	from, to int
}

// WholeDocument is the zero PageRange.
var WholeDocument = PageRange{}

// NewPageRange returns pages from..to inclusive.
func NewPageRange(from, to int) (PageRange, error) {
	if from == 0 && to == 0 {
		return WholeDocument, nil
	}
	if from < 1 || to < from {
		return PageRange{}, apperror.New(apperror.ValidationFailed, "page range must satisfy 1 <= from <= to")
	}
	return PageRange{from: from, to: to}, nil
}

func (r PageRange) From() int             { return r.from }
func (r PageRange) To() int               { return r.to }
func (r PageRange) IsWholeDocument() bool { return r == WholeDocument }

// overlaps reports whether two ranges of the same document share a page.
// The whole document overlaps everything.
func (r PageRange) overlaps(o PageRange) bool {
	if r.IsWholeDocument() || o.IsWholeDocument() {
		return true
	}
	return r.from <= o.to && o.from <= r.to
}

// KnowledgeRef points at pages of one knowledge-base document.
type KnowledgeRef struct {
	documentID uuid.UUID
	pages      PageRange
}

// NewKnowledgeRef returns a reference to pages of documentID.
func NewKnowledgeRef(documentID uuid.UUID, pages PageRange) (KnowledgeRef, error) {
	if documentID == uuid.Nil {
		return KnowledgeRef{}, apperror.New(apperror.ValidationFailed, "document id is required")
	}
	return KnowledgeRef{documentID: documentID, pages: pages}, nil
}

func (k KnowledgeRef) DocumentID() uuid.UUID { return k.documentID }
func (k KnowledgeRef) Pages() PageRange      { return k.pages }

// Source is where a pipeline's ideas and material come from.
type Source struct {
	kind         SourceKind
	instructions string
	refs         []KnowledgeRef
}

// NewPromptSource returns a source driven by instructions alone.
func NewPromptSource(instructions string) (Source, error) {
	instructions, err := parseInstructions(instructions)
	if err != nil {
		return Source{}, err
	}
	if instructions == "" {
		return Source{}, apperror.New(apperror.ValidationFailed, "a prompt pipeline needs instructions")
	}
	return Source{kind: SourcePrompt, instructions: instructions}, nil
}

// NewKnowledgeSource returns a source drawing on refs, with optional
// instructions. Several documents and several ranges of one document may
// be referenced; ranges of the same document must not overlap, so no
// page is fed to the agent twice.
func NewKnowledgeSource(instructions string, refs []KnowledgeRef) (Source, error) {
	instructions, err := parseInstructions(instructions)
	if err != nil {
		return Source{}, err
	}
	if len(refs) == 0 {
		return Source{}, apperror.New(apperror.ValidationFailed, "a knowledge pipeline needs at least one document reference")
	}
	if len(refs) > MaxKnowledgeRefs {
		return Source{}, apperror.New(apperror.ValidationFailed, "too many document references")
	}
	for i, a := range refs {
		if a.documentID == uuid.Nil {
			return Source{}, apperror.New(apperror.ValidationFailed, "document id is required")
		}
		for _, b := range refs[i+1:] {
			if a.documentID == b.documentID && a.pages.overlaps(b.pages) {
				return Source{}, apperror.New(apperror.ValidationFailed, "page ranges of the same document overlap")
			}
		}
	}
	return Source{kind: SourceKnowledge, instructions: instructions, refs: slices.Clone(refs)}, nil
}

// NewSEOSource returns a source that draws keyword opportunities.
// Instructions are required: they say what the content is for.
func NewSEOSource(instructions string) (Source, error) {
	instructions, err := parseInstructions(instructions)
	if err != nil {
		return Source{}, err
	}
	if instructions == "" {
		return Source{}, apperror.New(apperror.ValidationFailed, "an SEO source needs instructions describing the content")
	}
	return Source{kind: SourceSEO, instructions: instructions}, nil
}

func (s Source) Kind() SourceKind     { return s.kind }
func (s Source) Instructions() string { return s.instructions }
func (s Source) Refs() []KnowledgeRef { return slices.Clone(s.refs) }
func (s Source) isZero() bool         { return s.kind == "" }

func parseInstructions(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxInstructionsLength {
		return "", apperror.New(apperror.ValidationFailed, "instructions are too long")
	}
	return s, nil
}
