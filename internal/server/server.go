package server

import (
	"context"

	api "github.com/srinathLN7/proglog/api/v1"
	"google.golang.org/grpc"
)

type CommitLog interface {
	Append(*api.Record) (uint64, error)
	Read(uint64) (*api.Record, error)
}

type Config struct {
	CommitLog CommitLog
}

type grpcServer struct {
	api.UnimplementedLogServer
	*Config
}

var _ api.LogServer = (*grpcServer)(nil)

func newgrpcServer(config *Config) (srv *grpcServer, err error) {
	srv = &grpcServer{
		Config: config,
	}
	return srv, nil
}

// NewGRPCServer: creates a grpc server and registers the service to that server
func NewGRPCServer(config *Config, opts ...grpc.ServerOption) (*grpc.Server, error) {

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
	offset, err := s.CommitLog.Append(req.Record)
	if err != nil {
		return nil, err
	}

	return &api.ProduceResponse{Offset: offset}, nil
}

// Consume : takes in an offset and return the corresponding record
func (s *grpcServer) Consume(ctx context.Context, req *api.ConsumeRequest) (*api.ConsumeResponse, error) {
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
