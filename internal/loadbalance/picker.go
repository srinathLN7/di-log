package loadbalance

import (
	"strings"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

var _ base.PickerBuilder = (*Picker)(nil)

// Picker: Define a gRPC load balancer picker responsible for selecting a target subconnection (server)
// when making an RPC call to balance the load among multiple servers
type Picker struct {
	mu        sync.RWMutex
	leader    balancer.SubConn
	followers []balancer.SubConn
	current   uint64
}

// Build: Picker uses the builder pattern like resolvers. gRPC passes a map of
// subconnections with information about those subconnections via `PickerBuildInfo`
// to Build() to setup the picker-behind the scenes. gRPC connected to the addresses
// that our resolver discovered.
func (p *Picker) Build(buildInfo base.PickerBuildInfo) balancer.Picker {
	p.mu.Lock()
	defer p.mu.Unlock()

	var followers []balancer.SubConn

	// ReadySCs - ready sub-connections is a map from ready sub connections
	// to the addresses used to create them
	for sc, scInfo := range buildInfo.ReadySCs {
		isLeader := scInfo.
			Address.
			Attributes.
			Value("is_leader").(bool)
		if isLeader {
			p.leader = sc
		} else {
			followers = append(followers, sc)
		}
	}

	p.followers = followers
	return p
}

var _ balancer.Picker = (*Picker)(nil)

// Pick: gRPC gives Pick a balancer.PickInfo containing the RPC's name and context to
// help the picker know what subconnection to pick.
func (p *Picker) Pick(info balancer.PickInfo) (
	balancer.PickResult, error) {

	p.mu.RLock()
	defer p.mu.RUnlock()

	var result balancer.PickResult

	// The method selects the appropriate subconnection based on the RPC's name (FullMethodName field) and whether there are any follower subconnections available.
	// If the RPC's name contains "Produce" or there are no follower subconnections, the leader subconnection is selected. This logic is often used when some operations
	// are meant to be executed on the leader server or when there are no follower servers available. If the RPC's name contains "Consume," the method selects the
	// next follower subconnection using a round-robin algorithm implemented in the nextFollower method.
	if strings.Contains(info.FullMethodName, "Produce") || len(p.followers) == 0 {
		result.SubConn = p.leader
	} else if strings.Contains(info.FullMethodName, "Consume") {
		result.SubConn = p.nextFollower()
	}

	if result.SubConn == nil {
		return result, balancer.ErrNoSubConnAvailable
	}

	return result, nil
}

// nextFollower: implements round-robin algorithm to pick the next follower
func (p *Picker) nextFollower() balancer.SubConn {
	cur := atomic.AddUint64(&p.current, uint64(1))
	len := uint64(len(p.followers))
	idx := int(cur % len)
	return p.followers[idx]
}

// init: Register the Picker with gRPC service
func init() {
	balancer.Register(
		base.NewBalancerBuilder(Name, &Picker{}, base.Config{}),
	)
}
