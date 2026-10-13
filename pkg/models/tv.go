package models

// TVFeedItem is either a marker range or a whole scene. Matching markers take
// precedence over a whole-scene entry from the supplemental scene filter.
type TVFeedItem struct {
	Scene  *Scene
	Marker *SceneMarker
}
