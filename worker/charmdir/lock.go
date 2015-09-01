package charmdir

import "sync"

// Lock defines a read-write lock that can be interrupted as part of a
// coordinated shutdown process.
//
// Once a Lock is interrupted, Run() will exit and all other blocked calls will
// return false. At this point the Lock stops functioning and all subsequent
// operations will fail, returning false.
//
// Interrupting a Lock should be considered a destructive, non-recoverable
// operation on the Lock instance.
type Lock interface {
	// Run should be called from a separate goroutine or a Worker that is responsible for
	// managing the Lock. It returns when the Lock has been interrupted, after which the Lock
	// instance should no longer be used.
	Run()

	// RLock returns whether a read-lock is acquired.
	RLock(abort <-chan struct{}) bool

	// RUnlock returns whether a read-lock was released.
	RUnlock(abort <-chan struct{}) bool

	// Lock returns whether a write-lock was acquired.
	Lock(abort <-chan struct{}) bool

	// Unlock returns whether a write-lock was released.
	Unlock(abort <-chan struct{}) bool
}

type lock struct {
	commands chan *lockCommand

	// stop is used to shutdown the lock independently from the control channel
	// used by Lock interface callers.
	stop <-chan struct{}

	// abort is used to internally shutdown the lock if a command was interrupted.
	abort chan struct{}

	// state holds the number of readers if > 0, indicates "unlocked" when == 0, and
	// indicates "write-locked" if -1.
	state int

	// writeRequested indicates whether a write-lock has been enqueued, blocking
	// and enqueuing any further read-locks.
	writeRequested bool

	// pendingOps represents the enqueued, blocked lock operations.
	pendingOps []*lockCommand

	mu      sync.Mutex
	running bool
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

// NewLock creates a new Lock. The stop control channel may be closed to
// interrupt and shutdown the lock independently of any Lock method callers.
func NewLock(stop <-chan struct{}) Lock {
	return &lock{
		commands: make(chan *lockCommand),
		stop:     stop,
		abort:    make(chan struct{}),
	}
}

// RLock implements Lock.
func (l *lock) RLock(abort <-chan struct{}) bool {
	return l.do(abort, cmdRLock)
}

// RUnlock implements Lock.
func (l *lock) RUnlock(abort <-chan struct{}) bool {
	return l.do(abort, cmdRUnlock)
}

// Lock implements Lock.
func (l *lock) Lock(abort <-chan struct{}) bool {
	return l.do(abort, cmdLock)
}

// Unlock implements Lock.
func (l *lock) Unlock(abort <-chan struct{}) bool {
	return l.do(abort, cmdUnlock)
}

func (l *lock) do(abort <-chan struct{}, cmd cmdType) bool {
	var running bool
	l.mu.Lock()
	running = l.running
	l.mu.Unlock()

	if !running {
		return false
	}

	ch := make(chan bool)
	l.commands <- &lockCommand{cmdType: cmd, ch: ch}
	select {
	case <-abort:
		defer close(l.abort)
		return false
	case stopped := <-ch:
		if stopped {
			return false
		}
		return true
	}
}

// Run implements Lock.
func (l *lock) Run() {
	l.loop()
}

func (l *lock) loop() {
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()

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
		case <-l.stop:
			l.shutdown()
			return
		case <-l.abort:
			l.shutdown()
			return
		}
	}
}

func (l *lock) shutdown() {
	// TODO(cmars): I suspect a race condition among concurrent calls to `do` around a shutdown.
	l.mu.Lock()
	l.running = false
	l.mu.Unlock()

	for _, op := range l.pendingOps {
		select {
		case op.ch <- true:
		default:
		}
	}
}

func (l *lock) canExecute(cmd *lockCommand) bool {
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

func (l *lock) execute(cmd *lockCommand) {
	switch cmd.cmdType {
	case cmdRLock:
		l.state++
	case cmdRUnlock:
		// TODO: check that we didn't unlock when we weren't locked?
		if l.state > 0 {
			l.state--
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
		}
	default:
		panic("unknown command")
	}
	close(cmd.ch)
}
