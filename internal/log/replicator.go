package log

import (
	"sync"

	api "github.com/srinathLN7/proglog/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Replicator: Connects to other servers with the grpc client configured with the dial options.
// Calls the `produce` function to save a copy of the messages it consumes from other servers
// servers: map from server address to a channel
type Replicator struct {
	DialOptions []grpc.DialOption
	LocalServer api.LogClient

	logger *zap.Logger

	mu      sync.Mutex
	servers map[string]chan struct{}
	closed  bool
	close   chan struct{}
}
