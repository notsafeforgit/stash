package api

import (
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/coder/websocket"
)

func graphQLWebsocketTransport() transport.Websocket {
	return transport.Websocket{
		Implementation: transport.CoderWebsocketImplementation{
			AcceptOptions: websocket.AcceptOptions{
				// Authentication is handled by the router. Preserve support for
				// clients hosted separately from the Stash server.
				InsecureSkipVerify: true,
			},
		},
		KeepAlivePingInterval: 10 * time.Second,
	}
}
