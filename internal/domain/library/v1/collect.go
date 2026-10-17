package library

// Collect says which kinds a workspace saves from its drafts: composed
// music, Pexels videos, Pexels photos. The zero value saves nothing,
// which is the default.
type Collect struct {
	Music bool `json:"music"`
	Video bool `json:"video"`
	Photo bool `json:"photo"`
}

// Collects reports whether kind k is saved.
func (c Collect) Collects(k Kind) bool {
	switch k {
	case KindMusic:
		return c.Music
	case KindVideo:
		return c.Video
	case KindPhoto:
		return c.Photo
	}
	return false
}

// Any reports whether anything is saved.
func (c Collect) Any() bool { return c.Music || c.Video || c.Photo }
