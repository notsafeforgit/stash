package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PluginOperationKindV3 string

const (
	PluginOperationQueryV3    PluginOperationKindV3 = "QUERY"
	PluginOperationMutationV3 PluginOperationKindV3 = "MUTATION"
	operationMaxInputV3                             = 1 << 20
	operationMaxOutputV3                            = 4 << 20
)

var operationNameV3 = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func (k PluginOperationKindV3) IsValid() bool {
	return k == PluginOperationQueryV3 || k == PluginOperationMutationV3
}

func (k PluginOperationKindV3) MarshalGQL(w io.Writer) { fmt.Fprint(w, strconv.Quote(string(k))) }

func (k *PluginOperationKindV3) UnmarshalGQL(value interface{}) error {
	text, ok := value.(string)
	if !ok || !PluginOperationKindV3(text).IsValid() {
		return fmt.Errorf("invalid plugin operation kind %v", value)
	}
	*k = PluginOperationKindV3(text)
	return nil
}

type OperationConfigV3 struct {
	Kind        PluginOperationKindV3 `yaml:"kind"`
	Description *string               `yaml:"description"`
}

type PluginOperationV3 struct {
	Name string
	OperationConfigV3
}

func (c Config) operationsV3() []PluginOperationV3 {
	var ret []PluginOperationV3
	if c.v3 != nil {
		for name, operation := range c.v3.Operations {
			ret = append(ret, PluginOperationV3{Name: name, OperationConfigV3: operation})
		}
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Name < ret[j].Name })
	return ret
}

// OperationV3 dispatches only declared operations, with fixed routing arguments.
// QUERY is a read-only authoring contract, not a sandbox for trusted plugin code.
func (c *Cache) OperationV3(ctx context.Context, pluginID, name string, kind PluginOperationKindV3, input map[string]interface{}) (interface{}, error) {
	p := c.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	if !p.Enabled {
		return nil, fmt.Errorf("plugin %q is disabled", pluginID)
	}
	if p.APIVersion != 3 || !kind.IsValid() {
		return nil, fmt.Errorf("declared operations require a v3 plugin and valid operation kind")
	}
	declared := false
	for _, operation := range p.OperationsV3 {
		if operation.Name == name && operation.Kind == kind {
			declared = true
			break
		}
	}
	if !declared {
		return nil, fmt.Errorf("plugin %q has no %s operation %q", pluginID, kind, name)
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > operationMaxInputV3 {
		return nil, fmt.Errorf("operation input must be JSON of at most %d bytes", operationMaxInputV3)
	}
	timeout := time.Minute
	if kind == PluginOperationMutationV3 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := c.RunPlugin(ctx, pluginID, map[string]interface{}{
		"mode": "operation", "operation": name, "operation_type": strings.ToLower(string(kind)), "input": input,
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	encoded, err = json.Marshal(result)
	if err != nil || len(encoded) > operationMaxOutputV3 {
		return nil, fmt.Errorf("operation output must be JSON of at most %d bytes", operationMaxOutputV3)
	}
	return result, nil
}
