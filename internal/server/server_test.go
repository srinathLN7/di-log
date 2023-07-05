package server

import (
	"context"
	"flag"
	"io/ioutil"
	"net"
	"os"
	"testing"

	api "github.com/srinathLN7/proglog/api/v1"
	"github.com/srinathLN7/proglog/internal/auth"
	"github.com/srinathLN7/proglog/internal/config"
	"github.com/srinathLN7/proglog/internal/log"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

var debug = flag.Bool("debug", false, "enable observability for debugging.")

// TestMain: gives a place for setup that applies to all tests in the file
func TestMain(m *testing.M) {
	flag.Parse() // go test -v -debug=true
	if *debug {
		logger, err := zap.NewDevelopment()
		if err != nil {
			panic(err)
		}

		// set the development logger as the global logger
		zap.ReplaceGlobals(logger)
	}

	os.Exit(m.Run())
}

// TestServer: defines a list of test cases and then runs a subtest for each case
func TestServer(t *testing.T) {

	// create a table of tests
	for scenario, fn := range map[string]func(
		t *testing.T,
		rootClient api.LogClient,
		nobodyClient api.LogClient,
		config *Config,
	){
		"produce/consume a message to/from the log succeeds": testProduceConsume,
		"produce/consume stream succeeds":                    testProduceConsumeStream,
		"consume past log boundary fails":                    testConsumePastBoundary,
		"unauthorized fails":                                 testUnauthorizedProduceConsume,
	} {
		t.Run(scenario, func(t *testing.T) {
			rootClient,
				nobodyClient,
				config,
				teardown := setupTest(t, nil)
			defer teardown()
			fn(t, rootClient, nobodyClient, config)
		})
	}
}

// setupTest : sets up each test case
func setupTest(t *testing.T, fn func(*Config)) (
	rootClient api.LogClient,
	nobodyClient api.LogClient,
	cfg *Config,
	teardown func(),
) {
	t.Helper()

	// create a tcp listener
	// port 0 will automatically assign us a free port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// client-side - config our CA as the client's Root CA

	/*
		// without mutual TLS
		clientTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
			CAFile: config.CAFile,
		})

		// with mutual-TLS for client and server authentication
		clientTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
			CertFile: config.ClientCertFile, // add client cert file
			KeyFile:  config.ClientKeyFile,  // add client key file
			CAFile:   config.CAFile,
		})

		require.NoError(t, err)
		clientCreds := credentials.NewTLS(clientTLSConfig)
		cc, err := grpc.Dial(
			l.Addr().String(),
			grpc.WithTransportCredentials(clientCreds),
		)
		require.NoError(t, err)
		client = api.NewLogClient(cc)

	*/

	newClient := func(crtPath, keyPath string) (
		*grpc.ClientConn,
		api.LogClient,
		[]grpc.DialOption,
	) {
		tlsConfig, err := config.SetupTLSConfig(config.TLSConfig{
			CertFile: crtPath,
			KeyFile:  keyPath,
			CAFile:   config.CAFile,
			Server:   false,
		})
		require.NoError(t, err)
		tlsCreds := credentials.NewTLS(tlsConfig)
		opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
		conn, err := grpc.Dial(l.Addr().String(), opts...)
		require.NoError(t, err)
		client := api.NewLogClient(conn)
		return conn, client, opts
	}

	var rootConn *grpc.ClientConn
	rootConn, rootClient, _ = newClient(
		config.RootClientCertFile,
		config.RootClientKeyFile,
	)

	var nobodyConn *grpc.ClientConn
	nobodyConn, nobodyClient, _ = newClient(
		config.NobodyClientCertFile,
		config.NobodyClientKeyFile,
	)

	// server side config
	serverTLSConfig, err := config.SetupTLSConfig(config.TLSConfig{
		CertFile:      config.ServerCertFile,
		KeyFile:       config.ServerKeyFile,
		CAFile:        config.CAFile,
		ServerAddress: l.Addr().String(),
		Server:        true,
	})

	require.NoError(t, err)
	serverCreds := credentials.NewTLS(serverTLSConfig)

	dir, err := ioutil.TempDir("", "server-test")
	require.NoError(t, err)

	clog, err := log.NewLog(dir, log.Config{})
	require.NoError(t, err)

	// notice `clog` *log.Log implements CommitLog interface
	// embed the casbin-based authorization policy in the configuration
	authorizer := auth.New(config.ACLModelFile, config.ACLPolicyFile)

	cfg = &Config{
		CommitLog:  clog,
		Authorizer: authorizer,
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

	// teardown => stop server gracefully, close the client connection and listener, and remove the client log
	return rootClient, nobodyClient, cfg, func() {
		server.Stop()
		rootConn.Close()
		nobodyConn.Close()
		l.Close()
		clog.Remove()
	}
}

// testProduceConsume: tests if the producing and consuming works as expected
func testProduceConsume(t *testing.T, client, _ api.LogClient, cfg *Config) {
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
func testProduceConsumeStream(t *testing.T, client, _ api.LogClient, config *Config) {
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
func testConsumePastBoundary(t *testing.T, client, _ api.LogClient, config *Config) {
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

// testUnauthorizedProduceConsume: tests if the server throws authorization error when
// an unauthorized client connects to the server to produce and consume a log
func testUnauthorizedProduceConsume(
	t *testing.T,
	_,
	nobodyClient api.LogClient,
	config *Config,
) {

	ctx := context.Background()
	produce, err := nobodyClient.Produce(
		ctx,
		&api.ProduceRequest{
			Record: &api.Record{
				Value: []byte("hello world"),
			},
		},
	)

	if produce != nil {
		t.Fatalf("produce response should be nil")
	}

	gotCode, wantCode := status.Code(err), codes.PermissionDenied
	if gotCode != wantCode {
		t.Fatalf("got code: %d, want: %d", gotCode, wantCode)
	}

	consume, err := nobodyClient.Consume(ctx, &api.ConsumeRequest{
		Offset: 0,
	})

	if consume != nil {
		t.Fatalf("consume response should be nil")
	}

	gotCode, wantCode = status.Code(err), codes.PermissionDenied
	if gotCode != wantCode {
		t.Fatalf("got code: %d, want: %d", gotCode, wantCode)
	}
}
