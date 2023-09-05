package agent

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/soheilhy/cmux"
	"github.com/srinathLN7/proglog/internal/auth"
	"github.com/srinathLN7/proglog/internal/discovery"
	"github.com/srinathLN7/proglog/internal/log"
	"github.com/srinathLN7/proglog/internal/server"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Agent: runs on every service instance, setting up and connecting all the different components
type Agent struct {
	Config Config

	mux        cmux.CMux
	log        *log.DistributedLog
	server     *grpc.Server
	membership *discovery.Membership

	shutdown     bool
	shutdowns    chan struct{}
	shutdownLock sync.Mutex
}

// Config: represents config params for all components of the agent
type Config struct {
	ServerTLSConfig *tls.Config // tls config of the agent's local server presented to the clients
	PeerTLSConfig   *tls.Config // tls config of the peers that is served b/w servers so they can connect with and replicate eah other
	DataDir         string
	BindAddr        string
	RPCPort         int
	NodeName        string
	StartJoinAddrs  []string
	ACLModelFile    string
	ACLPolicyFile   string

	Bootstrap bool // To enable bootstrapping the Raft cluster
}

func (c Config) RPCAddr() (string, error) {
	host, _, err := net.SplitHostPort(c.BindAddr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}

// NewAgent: creates an agent service and runs a set of methods to setup
// and run each of the agent's components
func NewAgent(config Config) (*Agent, error) {
	a := &Agent{
		Config:    config,
		shutdowns: make(chan struct{}),
	}

	// run the following list of setup functions to setup every component of the agent service
	setup := []func() error{
		a.setupLogger,
		a.setupMux,
		a.setupLog,
		a.setupServer,
		a.setupMembership,
	}

	for _, fn := range setup {
		if err := fn(); err != nil {
			return nil, err
		}
	}

	go a.serve()

	return a, nil
}

func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		_ = a.Shutdown()
		return err
	}

	return nil
}

// setupLogger: sets up the global logger service for the agent
func (a *Agent) setupLogger() error {
	logger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}

	zap.ReplaceGlobals(logger)
	return nil
}

// setupMux: Creates a listener on our RPC adddress that'll accept both
// Raft and grpc connections and then creates the mux with the listener
// Multiplex (mux) allows yout to serve different services on the same port.
func (a *Agent) setupMux() error {
	addr, err := net.ResolveTCPAddr("tcp", a.Config.BindAddr)
	if err != nil {
		return err
	}
	rpcAddr := fmt.Sprintf(
		"%s:%d",
		addr.IP.String(),
		a.Config.RPCPort,
	)

	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}

	a.mux = cmux.New(ln)
	return nil
}

// setupLog: sets up the log component of the agent service
func (a *Agent) setupLog() error {
	raftLn := a.mux.Match(func(reader io.Reader) bool {

		// Configure the mux that matches Raft connections
		// Identify Raft connections by reading one byte and checking that the byte
		// matches the byte we setup our outgoing Raft connections to write in Stream Layer
		b := make([]byte, 1)
		if _, err := reader.Read(b); err != nil {
			return false
		}

		return bytes.Equal(b, []byte{byte(log.RaftRPC)})
	})

	// Configure the distributed log's Raft to use our multiplexed listener
	// and create the distributed log
	logConfig := log.Config{}
	logConfig.Raft.StreamLayer = log.NewStreamLayer(
		raftLn,
		a.Config.ServerTLSConfig,
		a.Config.PeerTLSConfig,
	)
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}

	logConfig.Raft.BindAddr = rpcAddr
	logConfig.Raft.LocalID = raft.ServerID(a.Config.NodeName)
	logConfig.Raft.Bootstrap = a.Config.Bootstrap

	a.log, err = log.NewDistributedLog(
		a.Config.DataDir,
		logConfig,
	)

	if err != nil {
		return err
	}

	if a.Config.Bootstrap {
		err = a.log.WaitForLeader(3 * time.Second)
	}

	return err
}

// setupServer: sets up the grpc server component of our agent service
func (a *Agent) setupServer() error {
	authorizer := auth.New(
		a.Config.ACLModelFile,
		a.Config.ACLPolicyFile,
	)

	serverConfig := &server.Config{
		CommitLog:   a.log,
		Authorizer:  authorizer,
		GetServerer: a.log,
	}

	var opts []grpc.ServerOption

	// add the local servers TLS config
	// notice we are using `grpc.Creds` to setup server credentials for incoming connections
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}

	var err error
	a.server, err = server.NewGRPCServer(serverConfig, opts...)
	if err != nil {
		return err
	}

	// We've multiplexed two connection types (Raft and gRPC) and we added a matcher
	// for the Raft connections (first byte == 1), we know all other connections must be gRPC connections
	// cmux.Any() matches any connections and then gRPC server serves on the multiplexed listener
	grpcLn := a.mux.Match(cmux.Any())
	go func() {
		if err := a.server.Serve(grpcLn); err != nil {
			_ = a.Shutdown()
		}
	}()

	return err
}

// setupMembership: sets up a `Replicator` with the grpc dial options needed to connect to other servers
// and a client for the replicator to connect to other servers, consume their data, and produce a copy
// of the data to the local server.
func (a *Agent) setupMembership() error {

	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}

	// var opts []grpc.DialOption

	// // setup connection peer's TLS creds for connections
	// if a.Config.PeerTLSConfig != nil {
	// 	opts = append(opts,
	// 		grpc.WithTransportCredentials(
	// 			credentials.NewTLS(a.Config.PeerTLSConfig),
	// 		))
	// }

	// // create a client connection to the given target
	// conn, err := grpc.Dial(rpcAddr, opts...)
	// if err != nil {
	// 	return err
	// }

	// client := api.NewLogClient(conn)
	// a.replicator = &log.Replicator{
	// 	DialOptions: opts,
	// 	LocalServer: client,
	// }

	// Create and setup the new discovery membership service.
	// Note replicator implements the `Handler` interface
	a.membership, err = discovery.NewMembership(a.log, discovery.Config{
		NodeName: a.Config.NodeName,
		BindAddr: a.Config.BindAddr,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddrs: a.Config.StartJoinAddrs,
	})

	return err
}

// Shutdown: shuts down the agent service by gracefully switching off all it's components
func (a *Agent) Shutdown() error {

	// shutdown mutex lock ensures that the agent will shut down once even
	// if the caller calls Shutdown() multiple times
	a.shutdownLock.Lock()
	defer a.shutdownLock.Unlock()

	// if it's already shutdown, break and return immediately
	if a.shutdown {
		return nil
	}

	a.shutdown = true
	close(a.shutdowns)

	// Close each component of the agent gracefully
	shutdown := []func() error{
		a.membership.Leave, // Leave the cluster membership to stop receiving discovery events and notice other servers. See pkg `discovery` for more details.
		func() error {
			a.server.GracefulStop() // Gracefully stop the server to stop the server accepting new connections and blocks until all pending RPCs have finished.
			return nil
		},

		a.log.Close, // Close the log service. See pkg `log` for more details.
	}

	for _, fn := range shutdown {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}
