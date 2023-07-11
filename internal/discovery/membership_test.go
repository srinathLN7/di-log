package discovery_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/serf/serf"
	. "github.com/srinathLN7/proglog/internal/discovery"
	"github.com/stretchr/testify/require"
	"github.com/travisjeffery/go-dynaport"
)

// handler: mock tracks how may times our Membership calls the Handler interface
// `Join()` and `Leave()` method along with the ID's and addresses
type handler struct {
	joins  chan map[string]string
	leaves chan string
}

func (h *handler) Join(id, addr string) error {
	if h.joins != nil {

		// send value to the `joins` channel
		h.joins <- map[string]string{
			"id":   id,
			"addr": addr,
		}
	}
	return nil
}

func (h *handler) Leave(id string) error {
	if h.leaves != nil {

		// send value to the `leaves` channel
		h.leaves <- id
	}
	return nil
}

// setupMember: sets up a new member by calling `NewMembership()` in the `discovery` pkg.
// Under a free port with member's length as the node name ensuring names are unique.
// If members length is 0, then it means the cluster is empty
func setupMember(t *testing.T, members []*Membership) (
	[]*Membership, *handler,
) {
	id := len(members)
	ports := dynaport.Get(1)
	addr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
	tags := map[string]string{
		"rpc_addr": addr,
	}

	c := Config{
		NodeName: fmt.Sprintf("%d", id),
		BindAddr: addr,
		Tags:     tags,
	}

	h := &handler{}

	// new cluster with no existing members
	if len(members) == 0 {

		// create a buffered channel of size 3
		h.joins = make(chan map[string]string, 3)
		h.leaves = make(chan string, 3)
	} else {
		c.StartJoinAddrs = []string{
			members[0].BindAddr,
		}
	}

	// Create a new membership
	// Note here: `h` of type `*handler` is passed as a type of `Handler` interface
	// This is possible because `handler` implements the `Join` and `Leave` method as defined in the `Handler` interface
	m, err := NewMembership(h, c)
	require.NoError(t, err)
	members = append(members, m)
	return members, h
}

// TestMembership: Sets up a cluster with multiple servers and checks if the
// membership returns all current servers after joining/leaving the cluster
func TestMembership(t *testing.T) {

	// setup three members i.e. serf instance
	m, handler := setupMember(t, nil)
	m, _ = setupMember(t, m)
	m, _ = setupMember(t, m)

	// handler `joins` and `leaves` channel tell us the number of times each event
	// happened and for what servers
	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 &&
			len(m[0].Members()) == 3 &&
			len(handler.leaves) == 0
	}, 3*time.Second, 250*time.Millisecond)

	// leave the third server ("id": "2") from the cluster
	require.NoError(t, m[2].Leave())

	require.Eventually(t, func() bool {
		return len(handler.joins) == 2 &&
			len(m[0].Members()) == 3 &&
			serf.StatusLeft == m[0].Members()[2].Status &&
			len(handler.leaves) == 1
	}, 3*time.Second, 250*time.Millisecond)

	// when reading the value from the `leaves` channel, id should match the third server
	require.Equal(t, fmt.Sprintf("%d", 2), <-handler.leaves)
}
