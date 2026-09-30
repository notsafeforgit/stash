package archive

import "strings"

const SourceRetentionVersion = "gallery-dl-retained-v1"
const MaxSourcePayloadBytes = 4 << 20

type sourceObject = map[string]interface{}

func sourceKeys(names string) map[string]bool {
	ret := make(map[string]bool)
	for _, name := range strings.Fields(names) {
		ret[name] = true
	}
	return ret
}

var sourceSecretKeys = sourceKeys("password passwd cookie cookies authorization access_token refresh_token api_key apikey client_secret headers session")
var retainedPrivateKeys = sourceKeys("_reddit _parent _url")
var profileRoles = sourceKeys("user author source_user retweeted_by quoted_by quote_by")
var twitterProfileFields = sourceKeys("id id_str rest_id name screen_name nick description location based_in url date created_at profile_image profile_banner profile_image_url profile_image_url_https profile_banner_url")
var redditProfileFields = sourceKeys("id name created created_utc icon_img snoovatar_img")
var redditPageFields = sourceKeys("id name display_name display_name_prefixed title url description public_description icon_img banner_img header_img community_icon")

func sanitizeSource(value interface{}) interface{} {
	switch value := value.(type) {
	case sourceObject:
		ret := make(sourceObject)
		for key, child := range value {
			if sourceSecretKeys[strings.ReplaceAll(strings.ToLower(key), "-", "_")] || (strings.HasPrefix(key, "_") && !retainedPrivateKeys[key]) {
				continue
			}
			ret[key] = sanitizeSource(child)
		}
		return ret
	case []interface{}:
		ret := make([]interface{}, len(value))
		for i, child := range value {
			ret[i] = sanitizeSource(child)
		}
		return ret
	default:
		return value
	}
}

func selectedSourceFields(value sourceObject, fields map[string]bool) sourceObject {
	ret := make(sourceObject)
	for key, child := range value {
		if fields[key] {
			ret[key] = child
		}
	}
	return ret
}

func retainProfile(value sourceObject, platform string) sourceObject {
	if platform == "reddit" {
		ret := selectedSourceFields(value, redditProfileFields)
		if _, exists := ret["created_utc"]; exists {
			delete(ret, "created")
		}
		if page, ok := value["subreddit"].(sourceObject); ok {
			ret["subreddit"] = selectedSourceFields(page, redditPageFields)
		}
		return ret
	}
	ret := selectedSourceFields(value, twitterProfileFields)
	sections := map[string]map[string]bool{
		"legacy": twitterProfileFields, "core": sourceKeys("name screen_name created_at"), "avatar": sourceKeys("image_url"),
	}
	for key, fields := range sections {
		if section, ok := value[key].(sourceObject); ok {
			clean := selectedSourceFields(section, fields)
			if entities, ok := section["entities"].(sourceObject); key == "legacy" && ok {
				clean["entities"] = selectedSourceFields(entities, sourceKeys("url description"))
			}
			ret[key] = clean
		}
	}
	if entities, ok := value["entities"].(sourceObject); ok {
		ret["entities"] = selectedSourceFields(entities, sourceKeys("url description"))
	}
	return ret
}

func sourceCategory(value sourceObject, inherited string) string {
	if category, exists := value["category"]; exists {
		inherited, _ = category.(string)
	}
	return inherited
}

func retainProfiles(value interface{}, platform string) interface{} {
	switch value := value.(type) {
	case []interface{}:
		ret := make([]interface{}, len(value))
		for i, child := range value {
			ret[i] = retainProfiles(child, platform)
		}
		return ret
	case sourceObject:
		platform = sourceCategory(value, platform)
		ret := make(sourceObject)
		for key, child := range value {
			site := platform
			if key == "_reddit" {
				site = "reddit"
			}
			if profile, ok := child.(sourceObject); ok && (site == "reddit" || site == "twitter") && profileRoles[key] {
				ret[key] = retainProfile(profile, site)
			} else {
				ret[key] = retainProfiles(child, site)
			}
		}
		return ret
	default:
		return value
	}
}
