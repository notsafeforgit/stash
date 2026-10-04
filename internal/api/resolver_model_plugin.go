package api

import (
	"strings"

	"github.com/stashapp/stash/pkg/plugin"
)

type pluginURLBuilder struct {
	BaseURL string
	Plugin  *plugin.Plugin
}

func (b pluginURLBuilder) entry() *string {
	entry := b.Plugin.UI.Entry
	if entry == "" {
		return nil
	}
	// Entry is served from the plugin's assets route. Strip a leading
	// slash so the join is clean.
	rel := strings.TrimPrefix(entry, "/")
	url := b.BaseURL + "/plugin/" + b.Plugin.ID + "/assets/" + rel
	return &url
}
