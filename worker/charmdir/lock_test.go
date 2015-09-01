// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package charmdir_test

import (
	"time"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"launchpad.net/tomb"

	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/worker/charmdir"
)

type LockSuite struct {
	l charmdir.Lock
	t *tomb.Tomb
}

var _ = gc.Suite(&LockSuite{})

func (s *LockSuite) SetUpTest(c *gc.C) {
	s.t = new(tomb.Tomb)
	s.l = charmdir.NewLock(s.t.Dying())
	go func() {
		s.l.Run()
		s.t.Done()
	}()
}

func (s *LockSuite) TearDownTest(c *gc.C) {
	s.t.Kill(nil)
	s.t.Wait()
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
		c.Assert(s.l.RUnlock(stop), jc.IsTrue)
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
		c.Assert(s.l.Unlock(stop), jc.IsTrue)
	}
}

func (s *LockSuite) TestRLockManyLockBlocked(c *gc.C) {
	stop := make(chan struct{})
	timer := time.NewTimer(coretesting.ShortWait)
	go func() {
		<-timer.C
		close(stop)
	}()

	someValue := "foo"

	for i := 0; i < 3; i++ {
		c.Assert(s.l.RLock(stop), jc.IsTrue)
	}

	done := make(chan struct{})
	go func() {
		c.Assert(s.l.Lock(stop), jc.IsTrue)
		someValue = "bar"
		close(done)
	}()

	for i := 0; i < 3; i++ {
		c.Assert(s.l.RUnlock(stop), jc.IsTrue)
		c.Assert(someValue, gc.Equals, "foo")
	}

	select {
	case <-done:
	case <-time.After(coretesting.ShortWait):
		c.Fatal("timed out waiting for evidence of write lock")
	}
	c.Assert(someValue, gc.Equals, "bar")
}

func (s *LockSuite) TestRLockAbortLock(c *gc.C) {
	stopRLock := make(chan struct{})
	go func() {
		<-time.After(coretesting.ShortWait)
		close(stopRLock)
	}()

	for i := 0; i < 3; i++ {
		c.Assert(s.l.RLock(stopRLock), jc.IsTrue)
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
	case <-time.After(coretesting.ShortWait):
		c.Fatal("timeout waiting for aborted write lock attempt")
	}

	intrLock := make(chan struct{})
	go func() {
		<-time.After(coretesting.ShortWait)
		close(intrLock)
	}()
	// Nothing works now because once the lock is aborted or stopped, it shuts
	// down.
	c.Assert(s.l.RLock(intrLock), jc.IsFalse)
	c.Assert(s.l.RUnlock(intrLock), jc.IsFalse)
	c.Assert(s.l.Lock(intrLock), jc.IsFalse)
	c.Assert(s.l.Unlock(intrLock), jc.IsFalse)
}
