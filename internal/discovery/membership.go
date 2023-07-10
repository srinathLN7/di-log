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

// New: Create a membership with the required configuration and event handler
func New(handler Handler, config Config) (*Membership, error) {
	c := &Membership{
		Config:  config,
		handler: handler,
		logger:  zap.L().Named("membership"),
	}

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
