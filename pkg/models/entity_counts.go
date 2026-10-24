package models

// EntityCountKey identifies one aggregate, including its hierarchy scope. It is
// also the request-local loader key: aliases with different depths cannot share
// a result, and fields not selected by the caller are never computed.
type EntityCountKey struct {
	ID    int
	Depth int
	Field EntityCountField
}

type EntityCountField string

const (
	EntityCountScenes     EntityCountField = "scenes"
	EntityCountImages     EntityCountField = "images"
	EntityCountGalleries  EntityCountField = "galleries"
	EntityCountGroups     EntityCountField = "groups"
	EntityCountPerformers EntityCountField = "performers"
	EntityCountMarkers    EntityCountField = "markers"
	EntityCountO          EntityCountField = "o"
	EntityHasImage        EntityCountField = "has_image"
)
