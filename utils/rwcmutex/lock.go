// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rwcmutex

import "launchpad.net/tomb"

// Lock defines a read-write lock that can be closed as part of a
// coordinated shutdown process.
//
// Once a Lock is closed, Run() will exit and all other blocked calls will
// return false. At this point the Lock stops functioning and all subsequent
// operations will fail, returning false.
//
// Closing a Lock should be considered a destructive, non-recoverable
// operation on the Lock instance.
type Lock struct {
	tomb tomb.Tomb

	commands chan *lockCommand

	// closing is used to internally shutdown the lock if a command was closed.
	closing chan struct{}

	// state holds the number of readers if > 0, indicates "unlocked" when == 0, and
	// indicates "write-locked" if -1.
	state int

	// writeRequested indicates whether a write-lock has been enqueued, blocking
	// and enqueuing any further read-locks.
	writeRequested bool

	// pendingOps represents the enqueued, blocked lock operations.
	pendingOps []*lockCommand
}

type lockCommand struct {
	cmdType cmdType
	ch      chan bool
}

type cmdType int

const (
	cmdRLock   cmdType = iota
	cmdRUnlock cmdType = iota
	cmdLock    cmdType = iota
	cmdUnlock  cmdType = iota
)

// NewLock creates a new Lock.
func NewLock() *Lock {
	lock := &Lock{
		commands: make(chan *lockCommand),
		closing:  make(chan struct{}),
	}
	go lock.loop()
	return lock
}

// Close closes the lock.
func (l *Lock) Close() error {
	l.tomb.Kill(nil)
	return l.tomb.Wait()
}

// RLock acquires a read-lock and returns whether it succeeded.
func (l *Lock) RLock(closing <-chan struct{}) bool {
	return l.do(closing, cmdRLock)
}

// RUnlock releases a read-lock and returns whether it succeeded.
func (l *Lock) RUnlock() bool {
	return l.do(nil, cmdRUnlock)
}

// Lock acquires a write-lock and returns whether it succeeded.
func (l *Lock) Lock(closing <-chan struct{}) bool {
	return l.do(closing, cmdLock)
}

// Unlock releases a write-lock and returns whether it succeeded.
func (l *Lock) Unlock() bool {
	return l.do(nil, cmdUnlock)
}

func (l *Lock) do(closing <-chan struct{}, cmd cmdType) bool {
	ch := make(chan bool)
	select {
	case <-l.tomb.Dying():
		return false
	case <-closing:
		close(l.closing)
		<-l.tomb.Dying()
		return false
	case l.commands <- &lockCommand{cmdType: cmd, ch: ch}:
	}
	select {
	case <-closing:
		close(l.closing)
		<-l.tomb.Dying()
		return false
	case stopped := <-ch:
		if stopped {
			return false
		}
		return true
	}
}

func (l *Lock) loop() {
	defer l.tomb.Done()

LOOP:
	for {
		var nextCmd *lockCommand

		select {
		case nextCmd = <-l.commands:
			switch nextCmd.cmdType {
			case cmdRLock:
				if l.state < 0 || l.writeRequested {
					l.pendingOps = append(l.pendingOps, nextCmd)
					continue LOOP
				}
				l.execute(nextCmd)
			case cmdLock:
				if l.state != 0 {
					l.pendingOps = append(l.pendingOps, nextCmd)
					continue LOOP
				}
				l.execute(nextCmd)
			case cmdUnlock, cmdRUnlock:
				l.execute(nextCmd)
				if len(l.pendingOps) > 0 {
					nextCmd = l.pendingOps[0]
					for l.canExecute(nextCmd) {
						l.execute(nextCmd)
						l.pendingOps = l.pendingOps[1:]
						if len(l.pendingOps) > 0 {
							nextCmd = l.pendingOps[0]
						} else {
							break
						}
					}
				}
			}
		case <-l.tomb.Dying():
			l.shutdown()
			return
		case <-l.closing:
			l.shutdown()
			return
		}
	}
}

func (l *Lock) shutdown() {
	for _, op := range l.pendingOps {
		select {
		case op.ch <- true:
		default:
		}
	}
}

func (l *Lock) canExecute(cmd *lockCommand) bool {
	switch cmd.cmdType {
	case cmdRLock:
		return true
	case cmdRUnlock:
		return l.state > 0
	case cmdLock:
		return l.state == 0
	case cmdUnlock:
		return true
	}
	panic("unknown command")
}

func (l *Lock) execute(cmd *lockCommand) {
	switch cmd.cmdType {
	case cmdRLock:
		l.state++
	case cmdRUnlock:
		// TODO: check that we didn't unlock when we weren't locked?
		if l.state > 0 {
			l.state--
		} else {
			panic("cannot read-unlock a non-read-locked lock")
		}
	case cmdLock:
		if l.state > 0 {
			l.writeRequested = true
		} else if l.state == 0 {
			l.writeRequested = false
			l.state--
		}
	case cmdUnlock:
		// TODO: check that we didn't unlock when we weren't locked?
		if l.state < 0 {
			l.state++
			// TODO: check that state didn't get less than -1?
		} else {
			panic("cannot write-unlock a non-write-locked lock")
		}
	default:
		panic("unknown command")
	}
	close(cmd.ch)
}
