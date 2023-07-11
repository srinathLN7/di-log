package discovery

import (
	"net"

	"github.com/hashicorp/serf/serf"
	"go.uber.org/zap"
)

// Membership: Type wrapping Serf to provide service discovery and cluster membership
type Membership struct {
	Config
	handler Handler
	serf    *serf.Serf
	events  chan serf.Event
	logger  *zap.Logger
}

type Config struct {
	NodeName       string
	BindAddr       string
	Tags           map[string]string
	StartJoinAddrs []string
}

// Handler: represents a component in the service that needs to know when a server joins/leaves the cluster
type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

// New: Create a new membership with the required configuration and event handler.
// Sets up a new serf instance every time this call is invoked
func NewMembership(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config:  config,
		handler: handler,
		logger:  zap.L().Named("membership"),
	}

	// spin up a new serf instance for every new membership
	if err := c.setupSerf(); err != nil {
		return nil, err
	}

	return c, nil
}

// setupSerf: Creates and sets up a serf instance and spin up an event handler to handle serf's events.
// NodeName: Defaults to hostname, if unspecified
// BindAddr and BindPort: addr and port for gossiping
// Tags: used for simple data informing how to handle this node
// EventCh: receive serf's events when a node joins/leaves the cluster
// StartJoinAddrs: used by serf's gossip protocol to config new nodes when joining existing cluster
func (m *Membership) setupSerf() (err error) {
	addr, err := net.ResolveTCPAddr("tcp", m.BindAddr)
	if err != nil {
		return err
	}

	config := serf.DefaultConfig()
	config.Init()
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port
	m.events = make(chan serf.Event)
	config.EventCh = m.events
	config.Tags = m.Tags
	config.NodeName = m.Config.NodeName
	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}

	go m.eventHandler()
	if m.StartJoinAddrs != nil {
		_, err = m.serf.Join(m.StartJoinAddrs, true)
		if err != nil {
			return err
		}
	}
	return nil
}

// eventHandler: runs in a loop reading events sent by serf into the events channel
func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					continue
				}
				m.handleJoin(member)
			}

		case serf.EventMemberLeave, serf.EventMemberFailed:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					return
				}
				m.handleLeave(member)
			}
		}
	}
}

func (m *Membership) handleJoin(member serf.Member) {
	if err := m.handler.Join(
		member.Name,
		member.Tags["rpc_addr"],
	); err != nil {
		m.logError(err, "failed to join", member)
	}
}

func (m *Membership) handleLeave(member serf.Member) {
	if err := m.handler.Leave(
		member.Name,
	); err != nil {
		m.logError(err, "failed to join", member)
	}
}

// isLocal: returns if the given serf member is the local member by checking the member's names
func (m *Membership) isLocal(member serf.Member) bool {
	return m.serf.LocalMember().Name == member.Name
}

// Members: returns the snapshot of the cluster's serf members at a point in-timie
func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}

// Leave: tells the specified member to leave the cluster
func (m *Membership) Leave() error {
	return m.serf.Leave()
}

// logError: logs the given error and message
func (m *Membership) logError(err error, msg string, member serf.Member) {
	m.logger.Error(
		msg,
		zap.Error(err),
		zap.String("name", member.Name),
		zap.String("rpc_addr", member.Tags["rpc_addr"]),
	)
}
