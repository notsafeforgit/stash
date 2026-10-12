package urlbuilders

type SceneMarkerURLBuilder struct {
	BaseURL  string
	SceneID  string
	MarkerID string
}

func NewSceneMarkerURLBuilder(baseURL, sceneID, markerID string) SceneMarkerURLBuilder {
	return SceneMarkerURLBuilder{
		BaseURL:  baseURL,
		SceneID:  sceneID,
		MarkerID: markerID,
	}
}

func (b SceneMarkerURLBuilder) GetStreamURL() string {
	return b.BaseURL + "/scene/" + b.SceneID + "/scene_marker/" + b.MarkerID + "/stream"
}

func (b SceneMarkerURLBuilder) GetPreviewURL() string {
	return b.BaseURL + "/scene/" + b.SceneID + "/scene_marker/" + b.MarkerID + "/preview"
}

func (b SceneMarkerURLBuilder) GetScreenshotURL() string {
	return b.BaseURL + "/scene/" + b.SceneID + "/scene_marker/" + b.MarkerID + "/screenshot"
}
