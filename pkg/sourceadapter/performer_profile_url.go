package sourceadapter

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var onlyFansProfilePath = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)(?:/.*)?$`)
var onlyFansNumericPath = regexp.MustCompile(`^[0-9]+$`)

// PerformerProfileURL normalizes links stored as performer metadata. OnlyFans
// campaign/trial links identify the profile in their first path component.
// Other sites and unrecognized URLs retain their original spelling. This must
// not change ProfileReference or persisted evidence keys: a metadata link does
// not establish account ownership, and historical source URLs remain evidence.
func PerformerProfileURL(value string) string {
	input := strings.TrimSpace(value)
	lower := strings.ToLower(input)
	if strings.HasPrefix(lower, "onlyfans.com/") || strings.HasPrefix(lower, "www.onlyfans.com/") {
		input = "https://" + input
	} else if strings.HasPrefix(lower, "//onlyfans.com/") || strings.HasPrefix(lower, "//www.onlyfans.com/") {
		input = "https:" + input
	}
	canonical, ok := CanonicalProfileURL(input)
	if !ok || len(value) > 4096 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return value
	}
	u, _ := url.Parse(canonical)
	if u.Host != "onlyfans.com" {
		return value
	}
	m := onlyFansProfilePath.FindStringSubmatch(u.EscapedPath())
	if m == nil || onlyFansNumericPath.MatchString(m[1]) || m[1] == "." || m[1] == ".." {
		return value
	}
	ref := nativeProfileHandle("onlyfans", m[1], "home explore search my login logout signup register terms privacy about help support contact api oauth redirect payments settings notifications messages forgot-password reset-password")
	if ref == nil {
		return value
	}
	return "https://onlyfans.com/" + ref.Value
}
