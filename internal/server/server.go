package server

import (
	"context"
	"time"

	api "github.com/srinathLN7/proglog/api/v1"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"

	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	objectWildcard = "*"
	produceAction  = "produce"
	consumeAction  = "consume"
)

// CommitLog: interface defined for dependency inversion useful for switching out implmentations
type CommitLog interface {
	Append(*api.Record) (uint64, error)
	Read(uint64) (*api.Record, error)
}

// Authorizer: interface to switch out authorization implementation
type Authorizer interface {
	Authorize(subject, object, action string) error
}

type Config struct {
	CommitLog   CommitLog
	Authorizer  Authorizer
	GetServerer GetServerer
}

type grpcServer struct {
	api.UnimplementedLogServer
	*Config
}

type subjectContextKey struct {
}

var _ api.LogServer = (*grpcServer)(nil)

// authenticate: interceptor (aka. middleware) reading the subject out of the client's cert
// and writes to the RPCs context. With interceptors, you can intercept and modify execution
// of each RPC call, allowing you to break the request handling into smaller, reusable chunks
func authenticate(ctx context.Context) (context.Context, error) {

	// retrieve the peer info from context if exists
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return ctx, status.New(codes.Unknown, "couldn't find peer info").Err()
	}

	// check peer authentication info of the transport layer
	// if no transport security is used
	if peer.AuthInfo == nil {
		return context.WithValue(ctx, subjectContextKey{}, ""), nil
	}

	// type cast the tlsInfo retrieved to `TLSInfo` struct
	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	subject := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	ctx = context.WithValue(ctx, subjectContextKey{}, subject)
	return ctx, nil
}

// subject: returns the client's certs subject name (`Subject: CN:""`) allowing us to identify a client
// The value is written to the subjectContextKey by the `authenticate` interceptor (middleware)
func subject(ctx context.Context) string {
	return ctx.Value(subjectContextKey{}).(string)
}

func newgrpcServer(config *Config) (srv *grpcServer, err error) {
	srv = &grpcServer{
		Config: config,
	}
	return srv, nil
}

// NewGRPCServer: creates a grpc server and registers the service to that server
func NewGRPCServer(config *Config, opts ...grpc.ServerOption) (*grpc.Server, error) {

	// congigure logging
	// use uber's zap logging library for structured logs
	logger := zap.L().Named("server")
	zapOpts := []grpc_zap.Option{
		grpc_zap.WithDurationField(
			func(duration time.Duration) zapcore.Field {
				return zap.Int64(
					"grpc.time_ns",
					duration.Nanoseconds(), // log the duration of each req in nanoseconds
				)
			},
		),
	}

	// configure traces
	// trace captures request lifecycles and tracks the request flow in the system
	// always sample => all req's traced
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	// configure views => specifying what OpenCensus will collect
	err := view.Register(ocgrpc.DefaultServerViews...)
	if err != nil {
		return nil, err
	}

	// append the server options to hook up the `authenticate` and `zap` interceptors to our grpc server
	// enabling the server to kick-off the authorization process along with observability (traces, metrics, logs)
	// tags => traces
	// zap => logs
	// stats => metrics - OpenCensus stat handler
	opts = append(opts,
		grpc.StreamInterceptor(
			grpc_middleware.ChainStreamServer(
				grpc_ctxtags.StreamServerInterceptor(),
				grpc_zap.StreamServerInterceptor(logger, zapOpts...),
				grpc_auth.StreamServerInterceptor(authenticate),
			)),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			grpc_ctxtags.UnaryServerInterceptor(),
			grpc_zap.UnaryServerInterceptor(logger, zapOpts...),
			grpc_auth.UnaryServerInterceptor(authenticate),
		)),
		grpc.StatsHandler(&ocgrpc.ServerHandler{}),
	)

	// remember variadic functions which uses slice internally to handle variable number of arguments
	// pack (... in prefix) and unpack operator (... in suffix)
	gsrv := grpc.NewServer(opts...)
	srv, err := newgrpcServer(config)
	if err != nil {
		return nil, err
	}
	api.RegisterLogServer(gsrv, srv)
	return gsrv, nil
}

// Produce: takes in a `record` and produce the corresponding offset
func (s *grpcServer) Produce(ctx context.Context, req *api.ProduceRequest) (*api.ProduceResponse, error) {

	// check if the client is authorized to connect to the server
	if err := s.Authorizer.Authorize(subject(ctx), objectWildcard, produceAction); err != nil {
		return nil, err
	}

	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}

	return &api.ProduceResponse{Offset: offset}, nil
}

// Consume : takes in an offset and return the corresponding record
func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (*api.ConsumeResponse, error) {

	// check if the client is authorized to connect to the server
	if err := s.Authorizer.Authorize(subject(ctx), objectWildcard, consumeAction); err != nil {
		return nil, err
	}
	record, err := s.CommitLog.Read(req.Offset)
	if err != nil {
		return nil, err
	}

	return &api.ConsumeResponse{Record: record}, nil
}

// ProduceStream: implements a bidirectional streaming RPC method `ProduceStream` defined in the `Log` service.
// It allows the client to stream data into the server's log and receive a response indicating whether each request succeeded or failed.
func (s *grpcServer) ProduceStream(stream api.Log_ProduceStreamServer) error {
	for {

		/*
			Implementation: The function starts an infinite loop to continuously receive messages from the client using the `stream.Recv()` method.
			Each received message is then passed to the `s.Produce()` method (defined above) to process the request and generate a response.
			The response is sent back to the client using the `stream.Send()` method and the loop continues until an error occurs during receiving or
			sending, in which case the function returns the error.
		*/
		req, err := stream.Recv()
		if err != nil {
			return err
		}

		// use the above-defined s.Produce() method to get the response
		res, err := s.Produce(stream.Context(), req)
		if err != nil {
			return err
		}

		if err = stream.Send(res); err != nil {
			return err
		}
	}
}

// ConsumeStream: implements a server-side streaming RPC method `ConsumeStream` defined in the `Log` service.
// allows the client to specify the offset of records to read from the server's log, and the server streams every record that follows.
func (s *grpcServer) ConsumeStream(req *api.ConsumeRequest, stream api.Log_ConsumeStreamServer) error {
	for {

		/*
			The function uses a `select` statement to either process the request or check if the streaming context is done (indicating the client has terminated the stream).
			Inside the loop, the `s.Consume()` method (defined above) is called to process the request and generate a response. If the request is successful, the response
			is sent back to the client using the `stream.Send()` method. The loop continues until the streaming context is done or an error occurs during sending,
			in which case the function returns.

			NOTE: no stream.Recv() is used here since the client does not continously send msg's. It only send one offset and the server starts streaming
			all logs from that offset
		*/

		// select statements are not sequential
		select {
		case <-stream.Context().Done():
			return nil
		default:
			res, err := s.Consume(stream.Context(), req)

			// type switch the error
			switch err.(type) {
			case nil:
			case api.ErrOffsetOutOfRange:
				continue
			default:
				return err
			}
			if err = stream.Send(res); err != nil {
				return err
			}
			req.Offset++
		}
	}
}

// GetServerer: Seperate interface created to get servers.
// A non-distributed log such as CommitLog doesn't know about server
type GetServerer interface {
	GetServers() ([]*api.Server, error)
}

// GetServers:
func (s *grpcServer) GetServers(
	ctx context.Context, req *api.GetServersRequest,
) (
	*api.GetServersResponse,
	error,
) {
	servers, err := s.GetServerer.GetServers()
	if err != nil {
		return nil, err
	}

	return &api.GetServersResponse{Servers: servers}, nil
}
