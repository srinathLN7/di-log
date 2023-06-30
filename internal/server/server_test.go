package server

import (
	"context"
	"io/ioutil"
	"net"
	"testing"

	api "github.com/srinathLN7/proglog/api/v1"
	"github.com/srinathLN7/proglog/internal/config"
	"github.com/srinathLN7/proglog/internal/log"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestServer: defines a list of test cases and then runs a subtest for each case
func TestServer(t *testing.T) {

	// create a table of tests
	for scenario, fn := range map[string]func(
		t *testing.T,
		client api.LogClient,
		config *Config,
	){
		"produce/consume a message to/from the log succeeds": testProduceConsume,
		"produce/consume stream succeeds":                    testProduceConsumeStream,
		"consume past log boundary fails":                    testConsumePastBoundary,
	} {
		t.Run(scenario, func(t *testing.T) {
			client, config, teardown := setupTest(t, nil)
			defer teardown()
			fn(t, client, config)
		})
	}
}

// setupTest : sets up each test case
func setupTest(t *testing.T, fn func(*Config)) (
	client api.LogClient,
	cfg *Config,
	teardown func(),
) {
	t.Helper()

	// create a tcp listener
	// port 0 will automatically assign us a free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// client-side config
	// config our CA as the client's Root CA
	clientTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CAFile: config.CAFile,
	})

	require.NoError(t, err)

	clientCreds := credentials.NewTLS(clientTLSConfig)
	cc, err := grpc.Dial(
		l.Addr().String(),
		grpc.WithTransportCredentials(clientCreds),
	)

	require.NoError(t, err)
	client = api.NewLogClient(cc)

	// server side config
	serverTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		ServerAddress: l.Addr().String(),
	})

	require.NoError(t, err)
	serverCreds := credentials.NewTLS(serverTLSConfig)

	dir, err := ioutil.TempDir("", "server-test")
	require.NoError(t, err)

	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)

	// notice *log.Log implements CommitLog interface
	cfg = &Config{
		CommitLog: clog,
	}

	if fn != nil {
		fn(cfg)
	}

	server, err := NewGRPCServer(cfg, grpc.Creds(serverCreds))
	require.NoError(t, err)

	// spin up a seperate go routine for each server
	// `Serve` method is a blocking call
	go func() {
		server.Serve(l)
	}()

	client = api.NewLogClient(cc)

	// teardown => stop server gracefully, close the client connection and listener, and remove the client log
	return client, cfg, func() {
		server.Stop()
		cc.Close()
		l.Close()
		clog.Remove()
	}
}

// testProduceConsume: tests if the producing and consuming works as expected
func testProduceConsume(t *testing.T, client api.LogClient, config *Config) {
	// get a non-nil empty context with no values and deadline
	ctx := context.Background()

	want := &api.Record{
		Value: []byte("hello world"),
	}

	produce, err := client.Produce(
		ctx,
		&api.ProduceRequest{
			Record: want,
		},
	)

	require.NoError(t, err)

	consume, err := client.Consume(ctx, &api.ConsumeRequest{
		Offset: produce.Offset,
	})

	require.NoError(t, err)
	require.Equal(t, want.Value, consume.Record.Value)
	require.Equal(t, want.Offset, consume.Record.Offset)
}

// testProduceConsumeStream: test streaming of testProduceConsume()
// produce and consume streams
func testProduceConsumeStream(t *testing.T, client api.LogClient, config *Config) {
	ctx := context.Background()

	records := []*api.Record{
		{
			Value:  []byte("first msg"),
			Offset: 0,
		},
		{
			Value:  []byte("second msg"),
			Offset: 1,
		}}

	// produce streams
	{
		stream, err := client.ProduceStream(ctx)
		require.NoError(t, err)
		for offset, record := range records {
			err = stream.Send(&api.ProduceRequest{
				Record: record,
			})

			require.NoError(t, err)
			res, err := stream.Recv()
			require.NoError(t, err)
			if res.Offset != uint64(offset) {
				t.Fatalf("got offset: %d, want: %d",
					res.Offset,
					offset,
				)
			}
		}
	}

	// consume streams
	{
		stream, err := client.ConsumeStream(ctx, &api.ConsumeRequest{Offset: 0})
		require.NoError(t, err)

		for i, record := range records {
			res, err := stream.Recv()
			require.NoError(t, err)
			require.Equal(t, res.Record, &api.Record{
				Value:  record.Value,
				Offset: uint64(i),
			})
		}
	}
}

// testConsumePastBoundary: tests if our server responds with error `ErrsetOutOfRange` if and when client tries
// to consume beyond log boundaries
func testConsumePastBoundary(t *testing.T, client api.LogClient, config *Config) {
	ctx := context.Background()

	produce, err := client.Produce(ctx, &api.ProduceRequest{
		Record: &api.Record{
			Value: []byte("hello world"),
		},
	})

	require.NoError(t, err)

	// try to consume a log whose offset doesn't exist
	consume, err := client.Consume(ctx, &api.ConsumeRequest{
		Offset: produce.Offset + 1,
	})

	if consume != nil {
		t.Fatal("consume not nil")
	}

	gotErr := grpc.Code(err)
	wantErr := grpc.Code(api.ErrOffsetOutOfRange{}.GRPCStatus().Err())

	// require.Equal(t, gotErr, wantErr)
	if gotErr != wantErr {
		t.Fatalf("got err: %v, want: %v", gotErr, wantErr)
	}
}
