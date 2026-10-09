package archive

import (
	"encoding/json"
	"strings"
)

// PostContentRetentionVersion omits counters and viewer/search state which do
// not describe a change to the archived post. Attachment and profile evidence,
// original text, publication dates and thread relationships remain retained.
const PostContentRetentionVersion = "post-content-v1"

var redditPostNoise = sourceKeys("score ups downs upvote_ratio num_comments num_crossposts num_reports view_count total_awards_received all_awardings awarders gilded gildings subreddit_subscribers search_tags likes saved visited clicked hidden can_gild can_mod_post user_reports mod_reports report_reasons send_replies hide_score")
var twitterPostNoise = sourceKeys("favorite_count favourite_count retweet_count quote_count reply_count bookmark_count view_count views favorited favourited retweeted bookmarked")
var instagramPostNoise = sourceKeys("like_count comment_count play_count view_count video_view_count has_liked has_saved")
var blueskyPostNoise = sourceKeys("likeCount repostCount replyCount quoteCount viewer")
var tiktokPostNoise = sourceKeys("diggCount shareCount commentCount playCount collectCount")

// RetainPostContent is a storage policy, separate from the producer transport
// policy. Existing outboxes remain acceptable after this storage change.
func RetainPostContent(raw []byte) (json.RawMessage, error) {
	value, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	return EncodeSourceJSON(retainPostContent(value, "", ""))
}

func retainPostContent(value any, platform, role string) any {
	switch value := value.(type) {
	case []any:
		ret := make([]any, len(value))
		for i, child := range value {
			ret[i] = retainPostContent(child, platform, role)
		}
		return ret
	case sourceObject:
		platform = strings.ToLower(sourceCategory(value, platform))
		// A profile's fields belong to its separate retention policy. In
		// particular, a publisher bio is not a post body or a nested tweet.
		if profileRoles[role] {
			return value
		}
		var noise map[string]bool
		switch platform {
		case "reddit":
			noise = redditPostNoise
		case "twitter":
			noise = twitterPostNoise
		case "instagram":
			noise = instagramPostNoise
		case "bluesky":
			noise = blueskyPostNoise
		case "tiktok":
			noise = tiktokPostNoise
		}
		ret := make(sourceObject, len(value))
		for key, child := range value {
			if noise[key] || (platform == "reddit" || platform == "twitter") && (key == "source_extractor_url" || key == "subcategory") {
				continue
			}
			site := platform
			if key == "_reddit" {
				site = "reddit"
			}
			ret[key] = retainPostContent(child, site, key)
		}
		// The same Reddit post is returned without user data by search and
		// with a user object by profile scraping. Its shared post body must
		// not change merely because one route also provided a profile.
		if platform == "reddit" && sourceTruthy(value["id"]) && (role == "" || role == "_reddit" || role == "_parent" || role == "crosspost_parent_list") {
			if _, exists := ret["user"]; !exists {
				ret["user"] = nil
			}
		}
		return ret
	default:
		return value
	}
}
