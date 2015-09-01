// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rwcmutex_test

import (
	"sync"
	"time"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/utils/rwcmutex"
)

type LockSuite struct {
	l *rwcmutex.Lock
}

var _ = gc.Suite(&LockSuite{})

func (s *LockSuite) SetUpTest(c *gc.C) {
	s.l = rwcmutex.NewLock()
}

func (s *LockSuite) TearDownTest(c *gc.C) {
	c.Assert(s.l.Close(), jc.ErrorIsNil)
}

func (s *LockSuite) TestRLockRUnlock(c *gc.C) {
	stop := make(chan struct{})
	timer := time.NewTimer(coretesting.ShortWait)
	go func() {
		<-timer.C
		close(stop)
	}()
	for i := 0; i < 3; i++ {
		c.Assert(s.l.RLock(stop), jc.IsTrue)
		c.Assert(s.l.RUnlock(), jc.IsTrue)
	}
}

func (s *LockSuite) TestLockUnlock(c *gc.C) {
	stop := make(chan struct{})
	timer := time.NewTimer(coretesting.ShortWait)
	go func() {
		<-timer.C
		close(stop)
	}()
	for i := 0; i < 3; i++ {
		c.Assert(s.l.Lock(stop), jc.IsTrue)
		c.Assert(s.l.Unlock(), jc.IsTrue)
	}
}

func (s *LockSuite) TestRLockManyLockBlocked(c *gc.C) {
	stop := make(chan struct{})
	timer := time.NewTimer(coretesting.ShortWait)
	go func() {
		<-timer.C
		close(stop)
	}()

	m := sync.Mutex{}
	someValue := "foo"

	for i := 0; i < 3; i++ {
		c.Assert(s.l.RLock(stop), jc.IsTrue)
	}

	done := make(chan struct{})
	go func() {
		c.Assert(s.l.Lock(stop), jc.IsTrue)
		m.Lock()
		someValue = "bar"
		m.Unlock()
		close(done)
	}()

	for i := 0; i < 3; i++ {
		c.Assert(s.l.RUnlock(), jc.IsTrue)
		m.Lock()
		c.Assert(someValue, gc.Equals, "foo")
		m.Unlock()
	}

	select {
	case <-done:
	case <-time.After(coretesting.LongWait):
		c.Fatal("timed out waiting for evidence of write lock")
	}
	c.Assert(someValue, gc.Equals, "bar")
}

func (s *LockSuite) TestRLockAbortLock(c *gc.C) {
	for i := 0; i < 3; i++ {
		c.Assert(s.l.RLock(nil), jc.IsTrue)
	}

	stopLock := make(chan struct{})
	done := make(chan struct{})
	go func() {
		c.Assert(s.l.Lock(stopLock), jc.IsFalse)
		close(done)
	}()

	close(stopLock)
	select {
	case <-done:
	case <-time.After(2 * coretesting.LongWait):
		c.Fatal("timeout waiting for aborted write lock attempt")
	}

	intrLock := make(chan struct{})
	go func() {
		<-time.After(coretesting.ShortWait)
		close(intrLock)
	}()
	// Nothing works now because once the lock is aborted or stopped, it shuts
	// down.
	c.Assert(s.l.RLock(nil), jc.IsFalse)
	c.Assert(s.l.RUnlock(), jc.IsFalse)
	c.Assert(s.l.Lock(nil), jc.IsFalse)
	c.Assert(s.l.Unlock(), jc.IsFalse)
}

func (s *LockSuite) TestConcurrentLocks(c *gc.C) {
	ch := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			c.Assert(s.l.Lock(nil), jc.IsTrue)
			c.Assert(s.l.Unlock(), jc.IsTrue)
			ch <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		select {
		case <-ch:
		case <-time.After(coretesting.LongWait):
			c.Fatal("timed out waiting for concurrent lockers to complete")
		}
	}
}

func (s *LockSuite) TestBlockedReadersOnShutdown(c *gc.C) {
	c.Assert(s.l.Lock(nil), jc.IsTrue)
	ch := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			c.Assert(s.l.RLock(nil), jc.IsFalse)
			ch <- struct{}{}
		}()
	}
	c.Assert(s.l.Close(), jc.ErrorIsNil)
	for i := 0; i < 10; i++ {
		select {
		case <-ch:
		case <-time.After(coretesting.LongWait):
			c.Fatal("timed out waiting for concurrent lockers to be aborted")
		}
	}
}

func (s *LockSuite) TestBlockedReadersOnUnlock(c *gc.C) {
	c.Assert(s.l.Lock(nil), jc.IsTrue)
	ch := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			c.Assert(s.l.RLock(nil), jc.IsTrue)
			ch <- struct{}{}
		}()
	}
	c.Assert(s.l.Unlock(), jc.IsTrue)
	for i := 0; i < 10; i++ {
		select {
		case <-ch:
		case <-time.After(coretesting.LongWait):
			c.Fatal("timed out waiting for concurrent lockers to be aborted")
		}
	}
}
