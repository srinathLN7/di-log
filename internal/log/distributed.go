package log

import (
	"os"
	"path/filepath"

	"github.com/hashicorp/raft"
)

// DistributedLog: Consits of Log and Config replicated with Raft
type DistributedLog struct {
	config Config
	log    *Log
	raft   *raft.Raft
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

func (l *DistributedLog) setupLog(dataDir string) error {
	logDir := filepath.Join(dataDir, "log")

	// `0755` means read and execute access for everyone and also write access for the owner of the file.
	// When you perform chmod 755 filename command you allow everyone to read and execute the file,
	// the owner is allowed to write to the file as well
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	var err error
	l.log, err = NewLog(logDir, l.config)
	return err
}
