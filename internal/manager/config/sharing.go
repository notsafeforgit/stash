package config

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

const SharingPublicURL = "sharing_public_url"

func (i *Config) GetSharingPublicURL() string { return i.getString(SharingPublicURL) }

func ValidateSharingPublicURL(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(value, "?#\\") || u.RawPath != "" || !strings.HasSuffix(u.Path, "/share") || path.Clean(u.Path) != u.Path {
		return fmt.Errorf("public sharing URL must be an absolute HTTPS URL ending in /share")
	}
	return nil
}
