package config

const (
	// EnableV3UI is a launch flag for the fork-owned v3 frontend and its
	// supporting API/routes. It intentionally defaults off so the upstream
	// v2.5 web UI and backend endpoints remain the default code path.
	EnableV3UI = "enable-v3-ui"
)

func (i *Config) GetEnableV3UI() bool {
	return i.getBool(EnableV3UI)
}
