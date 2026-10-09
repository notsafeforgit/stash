package archive

import (
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var catalogHandle = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var catalogNumericID = regexp.MustCompile(`^[0-9]+$`)
var catalogDID = regexp.MustCompile(`^did:[a-z0-9]+:[A-Za-z0-9._:%-]+$`)

// CatalogDirectoryAccounts understands the historical gallery-dl author folder
// templates. Feed/saved/subreddit directories and arbitrary display names are
// deliberately not author evidence. Mirror folders do not establish native IDs.
func CatalogDirectoryAccounts(label string) []models.AccountReference {
	parts := strings.Split(label, ", ")
	if len(parts) < 2 || len(parts) > 3 || !catalogHandle.MatchString(parts[0]) {
		return nil
	}
	service := parts[len(parts)-1]
	switch service {
	case "reddit", "twitter", "instagram", "bluesky", "tiktok":
	default:
		return nil
	}
	refs := []models.AccountReference{{Namespace: "native:" + service, Kind: "handle", Value: parts[0]}}
	if len(parts) == 3 {
		if service == "bluesky" {
			if !catalogDID.MatchString(parts[1]) {
				return nil
			}
		} else if service != "twitter" || !catalogNumericID.MatchString(parts[1]) {
			return nil
		}
		refs = append(refs, models.AccountReference{Namespace: "native:" + service, Kind: "id", Value: parts[1]})
	}
	for i, ref := range refs {
		var err error
		refs[i], err = NormalizeAccountReference(ref)
		if err != nil {
			return nil
		}
	}
	return refs
}

// Mirror directory names are public usernames/display labels, not the numeric
// user IDs of Patreon/Fansly/etc. Resolve them only inside the post's already
// known mirror namespace, against an existing account, without pairing IDs.
func CatalogMirrorDirectoryAccount(label, namespace string) *models.AccountReference {
	service := strings.Split(namespace, ":")
	parts := strings.Split(label, ", ")
	if !ValidAccountNamespace(namespace) || len(service) != 3 || service[0] != "mirror" || (service[1] != "coomer" && service[1] != "kemono") ||
		len(parts) != 2 || parts[1] != service[2] || !catalogHandle.MatchString(parts[0]) {
		return nil
	}
	return &models.AccountReference{Namespace: namespace, Kind: "catalog_label", Value: parts[0]}
}
