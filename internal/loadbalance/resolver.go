package loadbalance

import (
	"context"
	"fmt"
	"sync"

	api "github.com/srinathLN7/proglog/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// Resolver: Type we'll implement into
// gRPC's resolver.Builder and resolver.Resolver
// clientConn: client's connection that gRPC passes it to the resolver to update with the servers it discovers
// resolverConn: resolver's own client connection to the server so it can call GetServers() and get the servers
type Resolver struct {
	mu            sync.Mutex
	clientConn    resolver.ClientConn
	resolverConn  *grpc.ClientConn
	serviceConfig *serviceconfig.ParseResult
	logger        *zap.Logger
}

var _ resolver.Builder = (*Resolver)(nil)

// Build: Receives the data needed to build a resolver that can discover the servers (like the target address) and the client connection the resolver will update with the servers it discovers.
// `Build` and `ResolveNow` method works together to initialize and update the resolver, enabling dynamic server discovery and load balancing for gRPC client connections.
// The `Build` method sets up the resolver, while the `ResolveNow` method discovers and updates the list of available servers when called.
func (r *Resolver) Build(
	target resolver.Target,
	cc resolver.ClientConn,
	opts resolver.BuildOptions,
) (resolver.Resolver, error) {

	// init logger, client connection and dial options
	r.logger = zap.L().Named("resolver")
	r.clientConn = cc

	var dialOpts []grpc.DialOption

	// if secure dial credentials are provided
	if opts.DialCreds != nil {
		dialOpts = append(
			dialOpts,
			grpc.WithTransportCredentials(opts.DialCreds),
		)
	}

	// construct a service configuration JSON with a load balancing configuration
	// to specify the load balancing strategy
	r.serviceConfig = r.clientConn.ParseServiceConfig(
		fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, Name),
	)

	var err error

	// Establish a gRPC client connection to the target endpoint using the specified dial options
	r.resolverConn, err = grpc.Dial(target.Endpoint, dialOpts...)
	if err != nil {
		return nil, err
	}

	r.ResolveNow(resolver.ResolveNowOptions{})
	return r, nil
}

const Name = "proglog"

// Scheme: Returns the resolver's scheme identifier. When you call `grpc.Dial` gRPC parses
// out the scheme from the target address you gave it and tries to find a resolver that matches, defaulting
// to its DNS resolver. For ex. proglog://your-svc-addr
func (r *Resolver) Scheme() string {
	return Name
}

func init() {
	resolver.Register(&Resolver{})
}

var _ resolver.Resolver = (*Resolver)(nil)

// ResolveNow: gRPC calls this method to resovle the target , discover the servers,
// and update the client connection with the servers.
func (r *Resolver) ResolveNow(resolver.ResolveNowOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client := api.NewLogClient(r.resolverConn)

	// get cluster and then set on cc attributes
	ctx := context.Background()
	res, err := client.GetServers(ctx, &api.GetServersRequest{})
	if err != nil {
		r.logger.Error(
			"failed to resolve server",
			zap.Error(err),
		)
		return
	}

	var addrs []resolver.Address
	for _, server := range res.Servers {
		addrs = append(addrs, resolver.Address{
			Addr: server.RpcAddr,
			Attributes: attributes.New(
				"is_leader",
				server.IsLeader,
			),
		})
	}

	// update the client connection state with the discovered server addresses and the service configuration
	r.clientConn.UpdateState(resolver.State{
		Addresses:     addrs,
		ServiceConfig: r.serviceConfig,
	})
}

// Close: Closes the resolver
func (r *Resolver) Close() {
	if err := r.resolverConn.Close(); err != nil {
		r.logger.Error(
			"failed to close conn",
			zap.Error(err),
		)
	}
}
