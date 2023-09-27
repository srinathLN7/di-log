package log

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	api "github.com/srinathLN7/proglog/api/v1"
	"google.golang.org/protobuf/proto"

	raftboltdb "github.com/hashicorp/raft-boltdb"
)

// DistributedLog: Consits of Log and Config replicated with Raft
type DistributedLog struct {
	config  Config
	log     *Log
	raftLog *logStore
	raft    *raft.Raft
}

// NewDistributedLog: Builds a new distributed log with the Raft service
// using the specified config and data directory
func NewDistributedLog(dataDir string, config Config) (
	*DistributedLog,
	error,
) {
	l := &DistributedLog{
		config: config,
	}

	if err := l.setupLog(dataDir); err != nil {
		return nil, err
	}

	if err := l.setupRaft(dataDir); err != nil {
		return nil, err
	}

	return l, nil
}

// setupLog: Creates the server log where the server will store the user's records
func (l *DistributedLog) setupLog(dataDir string) error {
	logDir := filepath.Join(dataDir, "log")

	// When you perform chmod 755 filename command you allow everyone to read and execute the file,
	// only the owner is allowed to write to the file as well
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	var err error
	l.log, err = NewLog(logDir, l.config)
	return err
}

// setupRaft: Configures and creates the server's Raft instance - internally sets up
// -> FSM,
// -> Log store,
// -> stable store,
// -> snapshot store,
// -> transport
func (l *DistributedLog) setupRaft(dataDir string) error {

	var err error

	// Start with creating a finite-state machine (FSM) that applies the commands we give Raft
	fsm := &fsm{log: l.log}

	// Setup a log store where Raft stores the commands
	logDir := filepath.Join(dataDir, "raft", "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	logConfig := l.config
	logConfig.Segment.InitialOffset = 1
	l.raftLog, err = newLogStore(logDir, logConfig)
	if err != nil {
		return err
	}

	// Setup a stable store where Raft stores cluster's configuration
	// kvstore to store important Raft's metadata
	// For eg. server current term or candidate the server voted for
	// Bolt is an embedded and persisted kv database for Go
	stableStore, err := raftboltdb.NewBoltStore(
		filepath.Join(dataDir, "raft", "stable"),
	)

	if err != nil {
		return err
	}

	retain := 1

	// Setup snapshot store for Raft to receive and restore
	// data efficiently - compact snapshots of its data
	snapshotStore, err := raft.NewFileSnapshotStore(
		filepath.Join(dataDir, "raft"),
		retain,
		os.Stderr,
	)

	if err != nil {
		return err
	}

	maxPool := 5
	timeout := 10 * time.Second

	// transport: wraps a stream layer - a low-level stream abstraction - for Raft to connect with server's peers
	transport := raft.NewNetworkTransport(
		l.config.Raft.StreamLayer,
		maxPool,
		timeout,
		os.Stderr,
	)

	config := raft.DefaultConfig()

	// Local id is the unique id for this server - must set
	config.LocalID = l.config.Raft.LocalID

	// optional parameters
	if l.config.Raft.HeartbeatTimeout != 0 {
		config.HeartbeatTimeout = l.config.Raft.HeartbeatTimeout
	}

	if l.config.Raft.ElectionTimeout != 0 {
		config.ElectionTimeout = l.config.Raft.ElectionTimeout
	}

	if l.config.Raft.LeaderLeaseTimeout != 0 {
		config.LeaderLeaseTimeout = l.config.Raft.LeaderLeaseTimeout
	}

	if l.config.Raft.CommitTimeout != 0 {
		config.CommitTimeout = l.config.Raft.CommitTimeout
	}

	// Create the Raft instance and bootstrap the cluster
	l.raft, err = raft.NewRaft(
		config,
		fsm,
		l.raftLog,
		stableStore,
		snapshotStore,
		transport,
	)

	if err != nil {
		return err
	}

	// Check if the Raft has any existing state
	hasState, err := raft.HasExistingState(
		l.raftLog,
		stableStore,
		snapshotStore,
	)

	if err != nil {
		return err
	}

	// If `bootstrap` is set to true and no existing state exists
	// then bootstrap the cluster
	if l.config.Raft.Bootstrap && !hasState {
		config := raft.Configuration{
			Servers: []raft.Server{{
				ID:      config.LocalID,
				Address: raft.ServerAddress(l.config.Raft.BindAddr),
			}},
		}
		err = l.raft.BootstrapCluster(config).Error()
	}
	return err
}

// Append: appends the record to the log
func (l *DistributedLog) Append(record *api.Record) (uint64, error) {

	// We tell the Raft to apply a cmd (ProduceRequest) telling FSM to append the record to the log
	res, err := l.apply(
		AppendRequestType,
		&api.ProduceRequest{Record: record},
	)

	if err != nil {
		return 0, err
	}

	return res.(*api.ProduceResponse).Offset, nil
}

// apply : Wraps Raft's API to apply requests and return their responses
func (l *DistributedLog) apply(reqType RequestType, req proto.Message) (
	interface{},
	error,
) {
	var buf bytes.Buffer
	_, err := buf.Write([]byte{byte(reqType)})
	if err != nil {
		return nil, err
	}

	b, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}

	_, err = buf.Write(b)
	if err != nil {
		return nil, err
	}

	timeout := 10 * time.Second

	// Apply is used to apply a command to the FSM in a highly consistent manner. This returns a future that can be used to wait on the application.
	// An optional timeout can be provided to limit the amount of time we wait for the command to be started. This must be run on the leader or it will fail.
	// raft.Apply(): internally runs the steps described in the Log replication to replicate the record and append the record to the leader's log
	future := l.raft.Apply(buf.Bytes(), timeout)

	// future.Error returns an error when something goes wrong with Raft's replication
	if future.Error() != nil {
		return nil, future.Error()
	}

	// future.Response returns what the FSM's Apply() method returned
	res := future.Response()
	if err, ok := res.(error); ok {
		return nil, err
	}

	return res, nil
}

// Read: reads the record for the offset from the server's log
// For weaker consistency model -> read operations need not go through RAFT
// For stronger consistency model -> reads must be up-to-date with writes
func (l *DistributedLog) Read(offset uint64) (*api.Record, error) {
	return l.log.Read(offset)
}

// Finite State Machine -  Raft defers running the business logic to the FSM
// FSM implements three methods - Apply, Snapshot, Restore

// Ensure that the fsm struct implements the raft.FSM interface at compile-time.
var _ raft.FSM = (*fsm)(nil)

type fsm struct {
	log *Log
}

type RequestType uint8

const (
	AppendRequestType RequestType = 0
)

// Apply: Raft invokes this method after committing a log entry
func (l *fsm) Apply(record *raft.Log) interface{} {
	buf := record.Data

	// Extract the `reqType` from the first byte
	reqType := RequestType(buf[0])
	switch reqType {
	case AppendRequestType:
		return l.applyAppend(buf[1:])
	}

	return nil
}

// applyAppend: de-serialize, append, and then return the record to the local log
func (l *fsm) applyAppend(b []byte) interface{} {
	var req api.ProduceRequest
	err := proto.Unmarshal(b, &req)
	if err != nil {
		return err
	}

	offset, err := l.log.Append(req.Record)
	if err != nil {
		return err
	}

	return &api.ProduceResponse{Offset: offset}
}

// Snapshot: returns an FSMSnapshot that represents a point-in-time snapshot of the FSM's state
// Serves two purposes: Allow Raft to compact its log so it doesn't store logs whose commands Raft has already applied
// Allow Raft to bootstrap new servers more efficiently than if the leader had to replicate its entire log again and again
func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	r := f.log.Reader()
	return &snapshot{reader: r}, nil
}

// Ensure `snapshot` struct implements the `FSMSnapshot` interface
var _ raft.FSMSnapshot = (*snapshot)(nil)

type snapshot struct {
	reader io.Reader
}

// Persist: To write its state to some sink  - eg: in-memory, a file, or S3 bucket
func (s *snapshot) Persist(sink raft.SnapshotSink) error {
	// We use the file snapshot in here to store the btyes in
	if _, err := io.Copy(sink, s.reader); err != nil {
		_ = sink.Cancel()
		return err
	}

	return sink.Close()
}

// Release: Called when Raft is finished with the snapshot
func (s *snapshot) Release() {}

// Restore: restores an FSM from a snapshot FSM must discard existing state and must discard existing state to make sure its state will
// match the leader's replicated state. Reads a snapshot of serialized records, deserializes them, and appends them to the log of the fsm object.
// It ensures that the log's initial offset matches the state of the snapshot by updating the log's configuration based on the first record in the
// snapshot. This process allows the fsm object to recover its state from a previously saved snapshot.
func (f *fsm) Restore(r io.ReadCloser) error {

	// `b` reads data from the input reader `r`
	b := make([]byte, lenWidth)

	// `buf` is used to accumulate data read from the input reader for deserialization
	var buf bytes.Buffer

	for i := 0; ; i++ {

		// Reads the snapshot data from the reader until `EOF`
		_, err := io.ReadFull(r, b)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		size := int64(enc.Uint64(b))

		// Copies `size` bytes from the reader to the buffer
		if _, err = io.CopyN(&buf, r, size); err != nil {
			return err
		}

		// Deserialize the data in `buf` into a protocol buffer message of type `api.Record`
		//  Converts the binary data from the snapshot into a structured record
		record := &api.Record{}
		if err = proto.Unmarshal(buf.Bytes(), record); err != nil {
			return err
		}

		// Reset the log and configure its initial offset to the first
		// record's offset we read from the snapshot so the log's offset match
		if i == 0 {
			f.log.Config.Segment.InitialOffset = record.Offset
			if err := f.log.Reset(); err != nil {
				return err
			}
		}

		// Read the records in the snapshot and append them to our new log
		if _, err = f.log.Append(record); err != nil {
			return err
		}

		buf.Reset()
	}

	return nil
}

// Define Raft's log store

// Ensure `logStore` struct implements the `LogStore` interface
var _ raft.LogStore = (*logStore)(nil)

// We are using our own log as Raft's log store and hence we need to wrap
// our log to satisfy the LogStore interface Raft requires
type logStore struct {
	*Log
}

func newLogStore(dir string, c Config) (*logStore, error) {
	log, err := NewLog(dir, c)
	if err != nil {
		return nil, err
	}
	return &logStore{log}, nil
}

// Raft uses the below APIs to get records and information about the log

func (l *logStore) FirstIndex() (uint64, error) {
	return l.LowestOffset()
}

func (l *logStore) LastIndex() (uint64, error) {
	off, err := l.HighestOffset()
	return off, err
}

func (l *logStore) GetLog(index uint64, out *raft.Log) error {
	in, err := l.Read(index)
	if err != nil {
		return err
	}

	out.Data = in.Value
	out.Index = in.Offset
	out.Type = raft.LogType(in.Type)
	out.Term = in.Term
	return nil
}

func (l *logStore) StoreLog(record *raft.Log) error {
	return l.StoreLogs([]*raft.Log{record})
}

// StoreLogs: Translate the call to our log's API and our record type
func (l *logStore) StoreLogs(records []*raft.Log) error {
	for _, record := range records {
		if _, err := l.Append(&api.Record{
			Value: record.Data,
			Term:  record.Term,
			Type:  uint32(record.Type),
		}); err != nil {
			return err
		}
	}
	return nil
}

// DeleteRange: removes the records between the offsets
// To remove records that are old or stored in snapshot
func (l *logStore) DeleteRange(min, max uint64) error {
	return l.Truncate(max)
}

// Define the Raft stream layer

// Ensure the `StreamLayer` struct implements the `StreamLayer` interface
var _ raft.StreamLayer = (*StreamLayer)(nil)

// Define the stream layer type
type StreamLayer struct {
	ln              net.Listener
	serverTLSConfig *tls.Config
	peerTLSConfig   *tls.Config
}

// NewStreamLayer: Satifies raft.StreamLayer interface
func NewStreamLayer(
	ln net.Listener,
	serverTLSConfig,
	peerTLSConfig *tls.Config,
) *StreamLayer {
	return &StreamLayer{
		ln:              ln,
		serverTLSConfig: serverTLSConfig,
		peerTLSConfig:   peerTLSConfig,
	}
}

const RaftRPC = 1

// Dial: Makes outgoing connections to other servers in the Raft cluster
func (s *StreamLayer) Dial(
	addr raft.ServerAddress,
	timeout time.Duration,
) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	var conn, err = dialer.Dial("tcp", string(addr))
	if err != nil {
		return nil, err
	}

	// Identify to mux this is a raft rpc. When we connect to a server,
	// we write the Raft RPC byte to identify the connection type so
	// we can multiplex Raft on the same port as our Log grpc requests
	_, err = conn.Write([]byte{byte(RaftRPC)})
	if err != nil {
		return nil, err
	}

	if s.peerTLSConfig != nil {
		conn = tls.Client(conn, s.peerTLSConfig)
	}

	return conn, err
}

// Accept: mirror of Dial() - accepts incoming connection and reads the byte that
// identifies the connection and create a server-side TLS connection
func (s *StreamLayer) Accept() (net.Conn, error) {
	conn, err := s.ln.Accept()
	if err != nil {
		return nil, err
	}

	b := make([]byte, 1)

	// Read the Raft RPC byte from the connection type
	_, err = conn.Read(b)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal([]byte{byte(RaftRPC)}, b) {
		return nil, fmt.Errorf("not a raft rpc")
	}

	if s.serverTLSConfig != nil {
		return tls.Server(conn, s.serverTLSConfig), nil
	}

	return conn, nil
}

// Close : Closes the listener
func (s *StreamLayer) Close() error {
	return s.ln.Close()
}

// Addr: returns the listener's address
func (s *StreamLayer) Addr() net.Addr {
	return s.ln.Addr()
}

// Join: adds the server to the Raft cluster
func (l *DistributedLog) Join(id, addr string) error {
	configFuture := l.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return err
	}

	serverID := raft.ServerID(id)
	serverAddr := raft.ServerAddress(addr)

	// It checks if the current server's ID (srv.ID) matches the serverID or if its address (srv.Address) matches serverAddr.
	// This is done to determine if the server being added already exists in the configuration.
	// If both the ID and address match, it means the server has already joined the cluster, and the function returns nil, indicating success.
	// If either the ID or address (or both) match, it means there is a conflicting server entry. In this case, the existing server entry needs
	//  to be removed from the configuration.

	for _, srv := range configFuture.Configuration().Servers {
		if srv.ID == serverID || srv.Address == serverAddr {
			if srv.ID == serverID && srv.Address == serverAddr {
				// server has already joined
				return nil
			}

			// remove the existing server
			removeFuture := l.raft.RemoveServer(serverID, 0, 0)
			if err := removeFuture.Error(); err != nil {
				return err
			}
		}
	}

	// Add every server as a voter. Raft also supports adding servers as non-voters.
	// With non-voter servers if you wanted to replicate state to many servers to serve read only eventually consistent state
	// Everytime we add more voter servers, you increase the probability that replications and elections will take longer because
	// the leader has more servers it needs to communicate with to reach a majority.
	addFuture := l.raft.AddVoter(serverID, serverAddr, 0, 0)
	if err := addFuture.Error(); err != nil {
		return err
	}

	return nil
}

// Leave: removes the server from the cluster
// Removing the leader  will trigger a new election
func (l *DistributedLog) Leave(id string) error {
	removeFuture := l.raft.RemoveServer(raft.ServerID(id), 0, 0)
	return removeFuture.Error()
}

// WaitForLeader: Blocks until the cluster has elected a leader or times out
func (l *DistributedLog) WaitForLeader(timeout time.Duration) error {
	timeoutc := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutc:
			return fmt.Errorf("timed out")
		case <-ticker.C:
			if l := l.raft.Leader(); l != "" {
				return nil
			}
		}
	}
}

// Close: Shuts down the Raft instance and closes the local log
func (l *DistributedLog) Close() error {
	f := l.raft.Shutdown()
	if err := f.Error(); err != nil {
		return err
	}

	// Close raft logs.
	if err := l.raftLog.Log.Close(); err != nil {
		return err
	}

	// Close user logs.

	return l.log.Close()
}

// GetServers: Endpoint that exposes Raft's server data
// Raft config comprises the servers in the cluster, and includes server's ID, addr (incl. leader), and suffrage
func (l *DistributedLog) GetServers() ([]*api.Server, error) {
	future := l.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil, err
	}

	var servers []*api.Server
	for _, server := range future.Configuration().Servers {
		servers = append(servers, &api.Server{
			Id:       string(server.ID),
			RpcAddr:  string(server.Address),
			IsLeader: l.raft.Leader() == server.Address,
		})
	}

	return servers, nil
}
