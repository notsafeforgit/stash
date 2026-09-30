package archive

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceJSONPreservesExactIDsAndRejectsLossyInput(t *testing.T) {
	for _, raw := range []string{
		`{"id":98765432109876543210,"nested":{"a":null,"b":[-0,1.0,1e20]},"unicode":"Café 🎥 <&>"}`,
		`{"unicode":"\ud83c\udfa5","literal":"\\ud800"}`,
	} {
		body, err := DecodeJSONObject([]byte(raw), 65536)
		require.NoError(t, err)
		encoded, err := EncodeSourceJSON(body)
		require.NoError(t, err)
		decoded, err := DecodeJSONObject(encoded, 65536)
		require.NoError(t, err)
		require.Equal(t, body, decoded)
		if id, ok := body["id"]; ok {
			require.Equal(t, json.Number("98765432109876543210"), id)
			require.Contains(t, string(encoded), "98765432109876543210")
			require.Contains(t, string(encoded), "Café 🎥 <&>")
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{} {}`, `{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `{"nested":[{"a":null,"a":null}]}`,
		`{"bad":"\ud800"}`, `{"bad":"\udc00"}`, `{"bad":"\ud800\u1234"}`, "{\"bad\":\"\xff\"}",
		`{"deep":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}",
	} {
		_, err := DecodeJSONObject([]byte(raw), 65536)
		require.Error(t, err, "%s", raw)
	}
	_, err := DecodeJSONObject([]byte(`{"valid":true}`), 2)
	require.Error(t, err)
}
