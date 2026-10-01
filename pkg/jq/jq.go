package jq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/itchyny/gojq"
)

const (
	jqTimeout            = 250 * time.Millisecond
	MaxBytes             = 1 << 20
	jqMaxExpressionBytes = 16 << 10
	jqMaxResults         = 1024
	jqMaxMappings        = 128
)

func Compile(expression string) (*gojq.Code, error) {
	if len(expression) == 0 || len(expression) > jqMaxExpressionBytes {
		return nil, fmt.Errorf("jq expression must contain 1–%d bytes", jqMaxExpressionBytes)
	}
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression: %w", err)
	}
	// Deliberately omit module, environment and input loaders.
	return gojq.Compile(query)
}

// normalizeJQ accepts GraphQL's JSON numbers as well as native Go values, without
// losing integer precision or letting an expression mutate the caller's input.
func normalizeJQ(input interface{}, maxBytes int) (interface{}, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("jq input exceeds %d bytes", maxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	err = decoder.Decode(&value)
	return value, err
}

func evaluateJQ(ctx context.Context, expression string, input interface{}, limit, inputLimit int) ([]interface{}, error) {
	code, err := Compile(expression)
	if err != nil {
		return nil, err
	}
	input, err = normalizeJQ(input, inputLimit)
	if err != nil {
		return nil, err
	}
	results := []interface{}{}
	size := 0
	iter := code.RunWithContext(ctx, input)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := value.(error); ok {
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				break
			}
			return nil, fmt.Errorf("jq evaluation failed: %w", err)
		}
		if len(results) >= limit {
			return nil, fmt.Errorf("jq expression produced more than %d values; wrap multiple values in an array", limit)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		size += len(encoded)
		if size > MaxBytes {
			return nil, fmt.Errorf("jq output exceeds %d bytes", MaxBytes)
		}
		results = append(results, value)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// EvaluateJQ returns the jq output stream, including explicit nulls. Callers
// distinguish an empty stream from a single null or an array value.
func EvaluateJQ(ctx context.Context, expression string, input interface{}) ([]interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, jqTimeout)
	defer cancel()
	return evaluateJQ(ctx, expression, input, jqMaxResults, MaxBytes)
}

// EvaluateMappings is atomic: any invalid/multiple result fails the whole map.
func EvaluateMappings(ctx context.Context, mappings map[string]interface{}, input interface{}) (map[string]interface{}, error) {
	return EvaluateMappingsWithInputLimit(ctx, mappings, input, MaxBytes)
}

// EvaluateMappingsWithInputLimit shares the plugin evaluator with native source
// policies, whose retained source payload can exceed the plugin input limit.
// Output, time, expression and result limits remain unchanged.
func EvaluateMappingsWithInputLimit(ctx context.Context, mappings map[string]interface{}, input interface{}, inputLimit int) (map[string]interface{}, error) {
	if inputLimit <= 0 || inputLimit > 16<<20 {
		return nil, errors.New("jq input limit must be between 1 byte and 16 MiB")
	}
	if err := ValidateMappings(mappings); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, jqTimeout)
	defer cancel()
	ret := make(map[string]interface{})
	keys := make([]string, 0, len(mappings))
	for key := range mappings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values, err := evaluateJQ(ctx, mappings[key].(string), input, 1, inputLimit)
		if err != nil {
			return nil, fmt.Errorf("mapping %q: %w", key, err)
		}
		if len(values) == 1 {
			ret[key] = values[0]
		}
	}
	if _, err := normalizeJQ(ret, MaxBytes); err != nil {
		return nil, fmt.Errorf("mapping output: %w", err)
	}
	return ret, nil
}

func ValidateMappings(mappings map[string]interface{}) error {
	if mappings == nil || len(mappings) > jqMaxMappings {
		return fmt.Errorf("expected an object with at most %d jq mappings", jqMaxMappings)
	}
	for key, value := range mappings {
		expression, ok := value.(string)
		if key == "" || !ok {
			return fmt.Errorf("mapping %q must have a nonempty key and a jq string", key)
		}
		if _, err := Compile(expression); err != nil {
			return fmt.Errorf("mapping %q: %w", key, err)
		}
	}
	return nil
}
