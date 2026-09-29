package common

// HookSource identifies a parent plugin hook whose API call caused this event.
// Direct user/API operations have no parent hooks.
type HookSource struct {
	PluginID string `json:"pluginId"`
	Type     string `json:"type"`
}
