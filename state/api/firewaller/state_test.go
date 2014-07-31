// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package firewaller_test

import (
	"github.com/juju/names"
	gc "launchpad.net/gocheck"

	"github.com/juju/juju/instance"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
	apitesting "github.com/juju/juju/state/api/testing"
	statetesting "github.com/juju/juju/state/testing"
)

type stateSuite struct {
	firewallerSuite
	*apitesting.EnvironWatcherTests
}

var _ = gc.Suite(&stateSuite{})

func (s *stateSuite) SetUpTest(c *gc.C) {
	s.firewallerSuite.SetUpTest(c)
	s.EnvironWatcherTests = apitesting.NewEnvironWatcherTests(s.firewaller, s.BackingState, true)
}

func (s *stateSuite) TearDownTest(c *gc.C) {
	s.firewallerSuite.TearDownTest(c)
}

func (s *stateSuite) TestWatchEnvironMachines(c *gc.C) {
	w, err := s.firewaller.WatchEnvironMachines()
	c.Assert(err, gc.IsNil)
	defer statetesting.AssertStop(c, w)
	wc := statetesting.NewStringsWatcherC(c, s.BackingState, w)

	// Initial event.
	wc.AssertChange(s.machines[0].Id(), s.machines[1].Id(), s.machines[2].Id())

	// Add another machine make sure they are detected.
	otherMachine, err := s.State.AddMachine("quantal", state.JobHostUnits)
	c.Assert(err, gc.IsNil)
	wc.AssertChange(otherMachine.Id())

	// Change the life cycle of last machine.
	err = otherMachine.EnsureDead()
	c.Assert(err, gc.IsNil)
	wc.AssertChange(otherMachine.Id())

	// Add a container and make sure it's not detected.
	template := state.MachineTemplate{
		Series: "quantal",
		Jobs:   []state.MachineJob{state.JobHostUnits},
	}
	_, err = s.State.AddMachineInsideMachine(template, s.machines[0].Id(), instance.LXC)
	c.Assert(err, gc.IsNil)
	wc.AssertNoChange()

	statetesting.AssertStop(c, w)
	wc.AssertClosed()
}

func (s *stateSuite) TestGetMachinePorts(c *gc.C) {
	ports, err := s.firewaller.GetMachinePorts(s.machines[0].Tag(), names.NewNetworkTag(network.DefaultPublic))
	c.Assert(err, gc.ErrorMatches, "ports document .* not found")
	c.Assert(ports, gc.HasLen, 0)

	// Open some ports and check again.
	err = s.units[0].OpenPort("tcp", 1234)
	c.Assert(err, gc.IsNil)
	err = s.units[0].OpenPort("tcp", 4321)
	c.Assert(err, gc.IsNil)
	ports, err = s.firewaller.GetMachinePorts(s.machines[0].Tag(), names.NewNetworkTag(network.DefaultPublic))
	c.Assert(err, gc.IsNil)
	c.Assert(ports, gc.HasLen, 2)

}

func (s *stateSuite) TestGetMachinePortIds(c *gc.C) {
	portIds, err := s.firewaller.GetMachinePortIds(s.machines[0].Tag())
	c.Assert(err, gc.IsNil)
	c.Assert(portIds, gc.HasLen, 0)

	// Open some ports and check again.
	err = s.units[0].OpenPort("tcp", 1234)
	c.Assert(err, gc.IsNil)
	err = s.units[0].OpenPort("tcp", 4321)
	c.Assert(err, gc.IsNil)
	portIds, err = s.firewaller.GetMachinePortIds(s.machines[0].Tag())
	c.Assert(err, gc.IsNil)
	c.Assert(portIds, gc.HasLen, 1)

}
