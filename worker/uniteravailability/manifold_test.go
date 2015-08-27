// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package uniteravailability_test

import (
	"sync"
	"time"

	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
	dt "github.com/juju/juju/worker/dependency/testing"
	"github.com/juju/juju/worker/uniteravailability"
)

type ManifoldSuite struct {
	testing.IsolationSuite
	manifold    dependency.Manifold
	getResource dependency.GetResourceFunc
}

var _ = gc.Suite(&ManifoldSuite{})

func kill(w worker.Worker) error {
	w.Kill()
	return w.Wait()
}

func (s *ManifoldSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.manifold = uniteravailability.Manifold()
	s.getResource = dt.StubGetResource(dt.StubResources{})
}

type gate struct {
	mu    sync.Mutex
	state string
}

func (g *gate) get() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

func (g *gate) set(s string) {
	g.mu.Lock()
	g.state = s
	g.mu.Unlock()
}

func (s *ManifoldSuite) TestStartSuccess(c *gc.C) {
	worker, err := s.manifold.Start(s.getResource)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(worker, gc.NotNil)
	defer kill(worker)

	var consumer uniteravailability.UniterAvailabilityConsumer
	err = s.manifold.Output(worker, &consumer)
	c.Check(err, jc.ErrorIsNil)
	c.Check(consumer.EnterAvailable(), gc.Equals, false)

	var setter uniteravailability.UniterAvailabilitySetter
	err = s.manifold.Output(worker, &setter)
	c.Check(err, jc.ErrorIsNil)
	setter.SetAvailable(true)

	nConsumers := 10
	for i := 0; i < nConsumers; i++ {
		c.Check(consumer.EnterAvailable(), gc.Equals, true)
	}

	var g gate
	g.set("consumers have availability")

	beforeCS := make(chan struct{})
	afterCS := make(chan struct{})
	go func() {
		close(beforeCS)
		setter.NotAvailable()
		close(afterCS)
		g.set("no availability for you")
	}()
	<-beforeCS
	time.Sleep(coretesting.ShortWait)

	for i := 0; i < nConsumers; i++ {
		c.Check(g.get(), gc.Equals, "consumers have availability")
		consumer.ExitAvailable()
	}

	select {
	case <-afterCS:
		c.Check(g.get(), gc.Equals, "no availability for you")
		c.Check(consumer.EnterAvailable(), jc.IsFalse)
	case <-time.After(coretesting.ShortWait):
		c.Fatal("timed out waiting for setter to revoke availability")
	}
}

func (s *ManifoldSuite) TestOutputBadTarget(c *gc.C) {
	worker, err := s.manifold.Start(s.getResource)
	c.Assert(err, jc.ErrorIsNil)
	c.Check(worker, gc.NotNil)
	defer kill(worker)

	var state interface{}
	err = s.manifold.Output(worker, &state)
	c.Check(err.Error(), gc.Equals, "out should be a pointer to a UniterAvailabilityConsumer or a UniterAvailabilitySetter; is *interface {}")
	c.Check(state, gc.IsNil)
}
