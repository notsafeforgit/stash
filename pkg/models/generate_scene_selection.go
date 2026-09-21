package models

// GenerateSceneSelectionInput explicitly scopes generation to scenes matching
// a list filter. An empty selection means all scenes; nil means legacy scope.
type GenerateSceneSelectionInput struct {
	FindFilter     *FindFilterType `json:"find_filter,omitempty"`
	SceneFilterAST *FilterAST      `json:"scene_filter_ast,omitempty"`
}
