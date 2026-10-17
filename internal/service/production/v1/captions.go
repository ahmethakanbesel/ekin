package production

import (
	"context"
	"encoding/json"

	"kurgu/internal/apperror"
	storydomain "kurgu/internal/domain/story/v1"
	"kurgu/pkg/archive"
	"kurgu/pkg/caption"
)

// maxTimingsBytes bounds the narration timings read for captions: an
// hour of speech is well under it.
const maxTimingsBytes = 16 << 20

// captions makes a narrated revision's caption track (name is
// ArtifactCaptionsSRT or ArtifactCaptionsVTT) from the word timings in
// its working files.
func (s *Service) captions(ctx context.Context, rev storydomain.Revision, name string) ([]byte, error) {
	key, ok := rev.Artifacts[storydomain.ArtifactWorkdir]
	if !rev.Captioned() || !ok {
		return nil, apperror.New(apperror.NotFound, "the revision has no narration to caption")
	}
	rc, _, err := s.blobs.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	files, err := archive.ReadFiles(rc, maxTimingsBytes, narrationTimings)
	if err != nil {
		return nil, err
	}
	raw, ok := files[narrationTimings]
	if !ok {
		return nil, apperror.New(apperror.NotFound, "the revision kept no narration timings")
	}
	return captionTrack(raw, name)
}

// captionTrack writes timings.json's words as SubRip or WebVTT.
func captionTrack(timings []byte, name string) ([]byte, error) {
	var t struct {
		Words []caption.Word `json:"words"`
	}
	if err := json.Unmarshal(timings, &t); err != nil {
		return nil, apperror.Wrap(apperror.InternalError, err, "the narration timings are unreadable")
	}
	cues := caption.Cues(t.Words)
	if len(cues) == 0 {
		return nil, apperror.New(apperror.NotFound, "the narration has no timed words")
	}
	if name == storydomain.ArtifactCaptionsVTT {
		return caption.VTT(cues), nil
	}
	return caption.SRT(cues), nil
}
