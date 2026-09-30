package archive

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var accountNamespace = regexp.MustCompile(`^(?:(?:native|ytdl):[a-z0-9][a-z0-9_.-]*|(?:mirror|legacy):[a-z0-9][a-z0-9_.-]*:[a-z0-9][a-z0-9_.-]*)$`)
var accountIdentifierKind = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

func ValidAccountNamespace(namespace string) bool {
	return len(namespace) <= 128 && accountNamespace.MatchString(namespace)
}

// NormalizeAccountReference folds only known case-insensitive native handles.
// Opaque IDs, unknown services and mirror user IDs retain their exact spelling.
// Normalizing a locator is not evidence that two accounts belong to one person.
func NormalizeAccountReference(ref models.AccountReference) (models.AccountReference, error) {
	if (!ValidAccountNamespace(ref.Namespace) && ref.Namespace != "url") || !accountIdentifierKind.MatchString(ref.Kind) ||
		ref.Value == "" || len(ref.Value) > 2048 || !utf8.ValidString(ref.Value) ||
		strings.TrimSpace(ref.Value) != ref.Value || strings.IndexFunc(ref.Value, unicode.IsControl) >= 0 {
		return ref, errors.New("invalid qualified account identifier")
	}
	if ref.Namespace == "url" {
		value, valid := CanonicalProfileURL(ref.Value)
		if ref.Kind != "profile" || !valid {
			return ref, errors.New("invalid account profile locator")
		}
		ref.Value = value
	} else if ref.Kind == "handle" {
		switch ref.Namespace {
		case "native:reddit", "native:twitter", "native:instagram", "native:bluesky", "native:tiktok", "native:onlyfans", "native:fansly", "native:patreon", "native:tumblr":
			ref.Value = cases.Fold().String(norm.NFC.String(ref.Value))
		}
	}
	return ref, nil
}
