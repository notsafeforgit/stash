package jq

import (
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/utils"
)

// readableText shares the translation provider's display projection without
// changing source evidence or inferring a missing value. Bound native function
// work even when the expression constructs a string larger than its input.
func readableText(value any, _ []any) any {
	if value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return errors.New("readable_text requires a string or null")
	}
	if len(text) > MaxBytes {
		return fmt.Errorf("readable_text input exceeds %d bytes", MaxBytes)
	}
	return utils.ReadableText(text)
}
