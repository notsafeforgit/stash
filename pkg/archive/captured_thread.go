package archive

import (
	"errors"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

// Twitter identifiers are decimal uint64s. Compare their exact text, never a
// float or a mutable screen name. Zero is gallery-dl's absent-value sentinel.
func TwitterThreadID(value string) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == value
}

func threadID(fields ...capturedField) (string, error) {
	var ret string
	for _, field := range fields {
		value, err := capturedIdentifierValue(field)
		if err != nil {
			return "", err
		}
		if value == "" || value == "0" {
			continue
		}
		if !TwitterThreadID(value) || (ret != "" && ret != value) {
			return "", errors.New("invalid or contradictory Twitter thread identity")
		}
		ret = value
	}
	return ret, nil
}

// ExtractCapturedThread reads only the captured tweet's relationships. Quotes,
// retweets, feed-owner profiles and filenames never establish a reply chain.
func ExtractCapturedThread(raw []byte) (*models.SourceThreadFacts, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	data, path, category, err := capturedSourceContext(data)
	if err != nil || category != "twitter" {
		return nil, err
	}
	legacy, _ := data["legacy"].(sourceObject)
	if legacy == nil {
		legacy = data
	}
	author, _ := data["author"].(sourceObject)
	ret := &models.SourceThreadFacts{Namespace: "native:twitter"}
	ret.ConversationID, err = threadID(capturedFieldAt(data, path, "conversation_id"), capturedFieldAt(legacy, path, "conversation_id_str"))
	if err != nil || ret.ConversationID == "" {
		return nil, err
	}
	for _, pair := range []struct {
		target *string
		fields []capturedField
	}{
		{&ret.PostID, []capturedField{capturedFieldAt(data, path, "tweet_id"), capturedFieldAt(data, path, "rest_id"), capturedFieldAt(legacy, path, "id_str")}},
		{&ret.ConversationID, []capturedField{capturedFieldAt(data, path, "conversation_id"), capturedFieldAt(legacy, path, "conversation_id_str")}},
		{&ret.ReplyID, []capturedField{capturedFieldAt(data, path, "reply_id"), capturedFieldAt(legacy, path, "in_reply_to_status_id_str")}},
		{&ret.AuthorID, []capturedField{capturedFieldAt(author, path, "id"), capturedFieldAt(legacy, path, "user_id_str")}},
		{&ret.ReplyAuthorID, []capturedField{capturedFieldAt(data, path, "reply_user_id"), capturedFieldAt(legacy, path, "in_reply_to_user_id_str")}},
	} {
		*pair.target, err = threadID(pair.fields...)
		if err != nil {
			return nil, err
		}
	}
	if ret.PostID == "" || ret.ConversationID == "" || ret.AuthorID == "" {
		return nil, nil
	}
	post, _ := strconv.ParseUint(ret.PostID, 10, 64)
	root, _ := strconv.ParseUint(ret.ConversationID, 10, 64)
	parent, _ := strconv.ParseUint(ret.ReplyID, 10, 64)
	if root > post || parent >= post || (ret.ReplyID == "" && ret.ConversationID != ret.PostID) ||
		(ret.ReplyID != "" && ret.ConversationID == ret.PostID) || (ret.ReplyID == "" && ret.ReplyAuthorID != "") {
		return nil, errors.New("invalid Twitter reply ancestry")
	}
	return ret, nil
}
