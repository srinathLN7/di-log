## Distributed Log 

`distributed.go` file defines a package called `log` that implements a distributed log using the Raft consensus algorithm. Below is a step-by-step explanation of what the code does:

1. Import Required Packages:
   - The code imports various packages, including `net`, `io`, `os`, `time`, and `tls`, for handling network communication and file I/O.
   - It also imports packages related to Raft implementation (`github.com/hashicorp/raft`) and Protobuf (`google.golang.org/protobuf/proto`).

2. Define the `DistributedLog` Struct:
   - The `DistributedLog` struct represents a distributed log that consists of log entries and Raft-related configurations.

3. Create a New Distributed Log:
   - The `NewDistributedLog` function creates a new instance of `DistributedLog` by initializing its log and Raft configurations.
   - It returns a pointer to the created `DistributedLog` instance.

4. Setup Log Directory:
   - The `setupLog` method is responsible for creating the directory where log entries will be stored.
   - It uses `os.MkdirAll` to create directories with proper permissions.

5. Setup Raft Configuration:
   - The `setupRaft` method is responsible for configuring and creating the Raft instance.
   - It sets up the log store, snapshot store, transport, and various Raft configurations.
   - It also checks whether the server should bootstrap a new cluster if it's the first server.

6. Append Log Entries:
   - The `Append` method is used to append a record to the distributed log.
   - It uses the Raft consensus algorithm to replicate the record across the cluster and returns the offset at which the record is stored.

7. Apply Requests:
   - The `apply` method is a generic function that wraps Raft's API to apply requests and return their responses.

8. Read Log Entries:
   - The `Read` method is used to read a log entry from the distributed log by providing an offset.

9. Finite State Machine (FSM):
   - Raft requires a finite state machine (FSM) implementation. The code defines an `fsm` struct that satisfies the `raft.FSM` interface.
   - The FSM handles applying Raft log entries to the distributed log.

10. Snapshot:
    - The code defines the `Snapshot` method to create a point-in-time snapshot of the FSM's state.
    - Snapshots are used to compact the Raft log and bootstrap new servers efficiently.

11. Restore Snapshot:
    - The `Restore` method is used to restore an FSM from a snapshot.
    - It reads a snapshot of serialized records, deserializes them, and appends them to the log of the FSM object.

12. Raft Log Store:
    - The `logStore` struct implements the `raft.LogStore` interface required by the Raft package.
    - It translates calls from Raft to the log's API for storing and retrieving log entries.

13. Stream Layer:
    - The code defines a custom stream layer `StreamLayer` that satisfies the `raft.StreamLayer` interface.
    - It handles incoming and outgoing Raft RPC connections.
    - The `Dial` and `Accept` methods establish connections and identify Raft RPCs using a special byte.
    - TLS encryption is optionally supported for both client and server.

14. Join and Leave Cluster:
    - The `Join` method adds a server to the Raft cluster, ensuring uniqueness of the server by checking its ID and address.
    - The `Leave` method removes a server from the cluster, triggering a new election if the leader is removed.

15. WaitForLeader:
    - The `WaitForLeader` method blocks until a leader is elected in the Raft cluster or until a timeout occurs.

16. Close Distributed Log:
    - The `Close` method shuts down the Raft instance and closes the local log.

Overall, this code provides the foundation for building a distributed log system that uses the Raft consensus algorithm for replication and consistency. It handles log storage, Raft configuration, joining and leaving the cluster, and various Raft-related operations.
