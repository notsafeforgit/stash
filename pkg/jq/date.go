package jq

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var utcDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}(T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2}))?$`)

// utcDate projects known instants into a calendar date without using the host
// timezone or normalizing an invalid calendar value into a different date.
// Missing values stay null; callers decide whether that clears or omits a field.
func utcDate(value any, _ []any) any {
	if value == nil {
		return nil
	}
	invalid := errors.New("utc_date requires a valid YYYY-MM-DD date or RFC3339 timestamp with an explicit timezone")
	text, ok := value.(string)
	if !ok {
		return invalid
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) > 64 || !utcDatePattern.MatchString(text) {
		return invalid
	}
	format := time.DateOnly
	if len(text) != len(time.DateOnly) {
		format = time.RFC3339Nano
		if text[len(text)-1] != 'Z' {
			// time.Parse accepts offset hours of 24 and minutes of 60, despite
			// their being outside RFC3339's ranges. Reject them explicitly.
			zone := text[len(text)-6:]
			if zone[1:3] > "23" || zone[4:6] > "59" {
				return invalid
			}
		}
	}
	parsed, err := time.Parse(format, text)
	if err != nil || parsed.Year() < 1 || parsed.UTC().Year() < 1 || parsed.UTC().Year() > 9999 {
		return invalid
	}
	return parsed.UTC().Format(time.DateOnly)
}
