package log

import (
	"context"
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

// init() : uses lazy initialization technique to initialize the server map
// Lazy init reduces the APIs size and complexity and can be achivied using pointers and nil checks
func (r *Replicator) init() {
	if r.logger == nil {
		r.logger = zap.L().Named("replicator")
	}

	if r.servers == nil {
		r.servers = make(map[string]chan struct{})
	}

	if r.close == nil {
		r.close = make(chan struct{})
	}
}

// Join: adds the given server address to the list of servers to replicate and kicks off
// the `replicate` go routine to run the actual replication logic
func (r *Replicator) Join(name, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// initalize the server
	r.init()

	if r.closed {
		return nil
	}

	if _, ok := r.servers[name]; ok {

		// already the replicator contains the given server address for replication.
		// hence skip it
		return nil
	}

	r.servers[name] = make(chan struct{})

	// spin up a replicator
	go r.replicate(addr, r.servers[name])

	return nil
}

// replicate: Replicates the log from the connected servers to the local servers.
// Creates a client and opens up a stream to consume all logs on the server.
// Loop consumes the logs from the discovered server to save a copy and replicates
// until the server fails or leaves the cluster and the replicator closes the channel for that server
func (r *Replicator) replicate(addr string, leave chan struct{}) {

	// create a grpc client
	cc, err := grpc.Dial(addr, r.DialOptions...)
	if err != nil {
		return
	}

	defer cc.Close()

	client := api.NewLogClient(cc)
	ctx := context.Background()

	// consume the log stream from each connected server
	stream, err := client.ConsumeStream(ctx,
		&api.ConsumeRequest{
			Offset: 0,
		})
	if err != nil {
		r.logError(err, "failed to consume", addr)
		return
	}

	// once consumed from the connected server, decode the `Record` through `stream.Recv()`
	// since we are getting a `stream` of logs, it is better to spin up a seperate go routine
	// and process each log records seperately on to the `records` channel
	records := make(chan *api.Record)
	go func() {
		for {
			recv, err := stream.Recv()
			if err != nil {
				r.logError(err, "failed to receive", addr)
				return
			}

			records <- recv.Record
		}
	}()

	// REPLICATION LOGIC
	for {

		// select: chooses the first unblocking condition
		// NOT SEQUENTIAL like `switch`
		select {
		case <-r.close:
			return
		case <-leave:
			return

		// once the records are consumed, replicate the logs onto the local server
		// To do this, the replicator must not be closed and the connected server should not leave the cluster
		case record := <-records:
			_, err = r.LocalServer.Produce(ctx,
				&api.ProduceRequest{
					Record: record,
				},
			)
			if err != nil {
				r.logError(err, "failed to produce", addr)
			}
		}
	}
}

// logError: logs the given error and message
func (m *Replicator) logError(err error, msg, addr string) {
	m.logger.Error(
		msg,
		zap.Error(err),
		zap.String("addr", addr),
	)
}

// Leave: handles the server leaving the cluster
func (r *Replicator) Leave(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// lazy initialize the replicator
	r.init()

	// if the remote server (identified by `name`) is not connected to the replicator (local server)
	// then DO NOTHING
	if _, ok := r.servers[name]; !ok {
		return nil
	}

	//close the associated (remote) server's channel
	close(r.servers[name])

	//remove the server from the list of servers to replicate i.e. delete the associated key in the `servers` mapping
	delete(r.servers, name)

	return nil
}
