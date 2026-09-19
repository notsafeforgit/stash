package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"
)

// Both the upstream UI's legacy protocol and v3's graphql-transport-ws must
// survive the gqlgen transport migration, including a separately hosted UI.
func TestGraphQLWebsocketProtocols(t *testing.T) {
	for _, protocol := range []struct {
		name, request, response string
	}{
		{"graphql-ws", "start", "data"},
		{"graphql-transport-ws", "subscribe", "next"},
	} {
		t.Run(protocol.name, func(t *testing.T) {
			gql := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{}}))
			gql.AddTransport(graphQLWebsocketTransport())
			server := httptest.NewServer(gql)
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
				Subprotocols: []string{protocol.name},
				HTTPHeader:   http.Header{"Origin": {"https://separate-ui.example"}},
			})
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
			require.Equal(t, protocol.name, conn.Subprotocol())

			require.NoError(t, wsjson.Write(ctx, conn, map[string]string{"type": "connection_init"}))
			var message struct {
				Type    string          `json:"type"`
				ID      string          `json:"id"`
				Payload json.RawMessage `json:"payload"`
			}
			require.NoError(t, wsjson.Read(ctx, conn, &message))
			require.Equal(t, "connection_ack", message.Type)
			require.NoError(t, wsjson.Write(ctx, conn, map[string]any{
				"id": "1", "type": protocol.request,
				"payload": map[string]string{"query": "query { __typename }"},
			}))
			for {
				require.NoError(t, wsjson.Read(ctx, conn, &message))
				if message.Type != "ka" {
					break
				}
			}
			require.Equal(t, protocol.response, message.Type)
			require.Equal(t, "1", message.ID)
			require.JSONEq(t, `{"data":{"__typename":"Query"}}`, string(message.Payload))
			require.NoError(t, wsjson.Read(ctx, conn, &message))
			require.Equal(t, "complete", message.Type)
		})
	}
}
