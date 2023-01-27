package db

import (
	"fmt"
	"net"

	"github.com/google/uuid"

	"bastionzero.com/bctl/v1/bctl/daemon/plugin/db/actions/dial"
	"bastionzero.com/bctl/v1/bctl/daemon/plugin/db/actions/pwdb"
	"bastionzero.com/bctl/v1/bzerolib/logger"
	"bastionzero.com/bctl/v1/bzerolib/plugin"
	bzdb "bastionzero.com/bctl/v1/bzerolib/plugin/db"
	smsg "bastionzero.com/bctl/v1/bzerolib/stream/message"
)

// Perhaps unnecessary but it is nice to make sure that each action is implementing a common function set
type IDbDaemonAction interface {
	ReceiveStream(stream smsg.StreamMessage)
	ReceiveMrtap(action string, actionPayload []byte) error
	Start(lconn *net.TCPConn) error
	Done() <-chan struct{}
	Err() error
	Kill(err error)
}

type DbDaemonPlugin struct {
	logger *logger.Logger

	action   IDbDaemonAction
	doneChan chan struct{}
	killed   bool

	targetUser string
	targetId   string

	// outbox
	outboxQueue chan plugin.ActionWrapper

	// Db-specific vars
	sequenceNumber int
}

func New(logger *logger.Logger, targetUser string, targetId string) *DbDaemonPlugin {
	return &DbDaemonPlugin{
		logger:         logger,
		doneChan:       make(chan struct{}),
		targetUser:     targetUser,
		targetId:       targetId,
		outboxQueue:    make(chan plugin.ActionWrapper, 5),
		sequenceNumber: 0,
	}
}

func (d *DbDaemonPlugin) StartAction(action bzdb.DbAction, conn *net.TCPConn) error {
	if d.killed {
		return fmt.Errorf("plugin has already been killed, cannot create a new shell action")
	}

	requestId := uuid.New().String()
	actLogger := d.logger.GetActionLogger(string(action))

	switch action {
	case bzdb.Dial:
		d.action = dial.New(actLogger, requestId, d.targetUser, d.targetId, d.outboxQueue, d.doneChan)
	case bzdb.Pwdb:
		d.action = pwdb.New(actLogger, d.targetUser, d.targetId, d.outboxQueue, d.doneChan)
	default:
		return fmt.Errorf("unrecognized db action: %s", action)
	}

	// send local tcp connection to action
	if err := d.action.Start(conn); err != nil {
		return fmt.Errorf("failed to start %s action: %w", action, err)
	}

	d.logger.Infof("db plugin created %s action", action)

	return nil
}

func (d *DbDaemonPlugin) Kill(err error) {
	d.killed = true
	if d.action != nil {
		d.action.Kill(err)
	}
}

func (d *DbDaemonPlugin) Done() <-chan struct{} {
	return d.doneChan
}

func (d *DbDaemonPlugin) Err() error {
	return d.action.Err()
}

func (d *DbDaemonPlugin) Outbox() <-chan plugin.ActionWrapper {
	return d.outboxQueue
}

func (d *DbDaemonPlugin) ReceiveStream(smessage smsg.StreamMessage) {
	d.logger.Tracef("db plugin received %s stream", smessage.Type)

	if d.action != nil {
		d.action.ReceiveStream(smessage)
	} else {
		d.logger.Debugf("ignoring received stream message because it was sent before an action was created")
	}
}

func (d *DbDaemonPlugin) ReceiveMrtap(action string, actionPayload []byte) error {
	if d.action != nil {
		return d.action.ReceiveMrtap(action, actionPayload)
	} else {
		d.logger.Debugf("ignoring received mrtap because it was sent before an action was created")
		return nil
	}
}
