// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package agent

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/juju/juju/agent"
	agenttools "github.com/juju/juju/agent/tools"
	apirsyslog "github.com/juju/juju/api/rsyslog"
	agenttesting "github.com/juju/juju/cmd/jujud/agent/testing"
	cmdutil "github.com/juju/juju/cmd/jujud/util"
	envtesting "github.com/juju/juju/environs/testing"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/tools"
	"github.com/juju/juju/version"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/rsyslog"
	"github.com/juju/juju/worker/upgrader"
)

type UnitSuite struct {
	coretesting.GitSuite
	agenttesting.AgentSuite
}

var _ = gc.Suite(&UnitSuite{})

func (s *UnitSuite) SetUpSuite(c *gc.C) {
	s.GitSuite.SetUpSuite(c)
	s.AgentSuite.SetUpSuite(c)
}

func (s *UnitSuite) TearDownSuite(c *gc.C) {
	s.AgentSuite.TearDownSuite(c)
	s.GitSuite.TearDownSuite(c)
}

func (s *UnitSuite) SetUpTest(c *gc.C) {
	s.GitSuite.SetUpTest(c)
	s.AgentSuite.SetUpTest(c)
}

func (s *UnitSuite) TearDownTest(c *gc.C) {
	s.AgentSuite.TearDownTest(c)
	s.GitSuite.TearDownTest(c)
}

const initialUnitPassword = "unit-password-1234567890"

// primeAgent creates a unit, and sets up the unit agent's directory.
// It returns the assigned machine, new unit and the agent's configuration.
func (s *UnitSuite) primeAgent(c *gc.C) (*state.Machine, *state.Unit, agent.Config, *tools.Tools) {
	jujutesting.AddStateServerMachine(c, s.State)
	svc := s.AddTestingService(c, "wordpress", s.AddTestingCharm(c, "wordpress"))
	unit, err := svc.AddUnit()
	c.Assert(err, jc.ErrorIsNil)
	err = unit.SetPassword(initialUnitPassword)
	c.Assert(err, jc.ErrorIsNil)
	// Assign the unit to a machine.
	err = unit.AssignToNewMachine()
	c.Assert(err, jc.ErrorIsNil)
	id, err := unit.AssignedMachineId()
	c.Assert(err, jc.ErrorIsNil)
	machine, err := s.State.Machine(id)
	c.Assert(err, jc.ErrorIsNil)
	inst, md := jujutesting.AssertStartInstance(c, s.Environ, id)
	err = machine.SetProvisioned(inst.Id(), agent.BootstrapNonce, md)
	c.Assert(err, jc.ErrorIsNil)
	conf, tools := s.PrimeAgent(c, unit.Tag(), initialUnitPassword, version.Current)
	return machine, unit, conf, tools
}

func (s *UnitSuite) newAgent(c *gc.C, unit *state.Unit) *UnitAgent {
	a := NewUnitAgent(nil, nil)
	s.InitAgent(c, a, "--unit-name", unit.Name(), "--log-to-stderr=true")
	err := a.ReadConfig(unit.Tag().String())
	c.Assert(err, jc.ErrorIsNil)
	return a
}

func (s *UnitSuite) TestParseSuccess(c *gc.C) {
	a := NewUnitAgent(nil, nil)
	err := coretesting.InitCommand(a, []string{
		"--data-dir", "jd",
		"--unit-name", "w0rd-pre55/1",
		"--log-to-stderr",
	})

	c.Assert(err, gc.IsNil)
	c.Check(a.AgentConf.DataDir(), gc.Equals, "jd")
	c.Check(a.UnitName, gc.Equals, "w0rd-pre55/1")
}

func (s *UnitSuite) TestParseMissing(c *gc.C) {
	uc := NewUnitAgent(nil, nil)
	err := coretesting.InitCommand(uc, []string{
		"--data-dir", "jc",
	})

	c.Assert(err, gc.ErrorMatches, "--unit-name option must be set")
}

func (s *UnitSuite) TestParseNonsense(c *gc.C) {
	for _, args := range [][]string{
		{"--unit-name", "wordpress"},
		{"--unit-name", "wordpress/seventeen"},
		{"--unit-name", "wordpress/-32"},
		{"--unit-name", "wordpress/wild/9"},
		{"--unit-name", "20/20"},
	} {
		err := coretesting.InitCommand(NewUnitAgent(nil, nil), append(args, "--data-dir", "jc"))
		c.Check(err, gc.ErrorMatches, `--unit-name option expects "<service>/<n>" argument`)
	}
}

func (s *UnitSuite) TestParseUnknown(c *gc.C) {
	err := coretesting.InitCommand(NewUnitAgent(nil, nil), []string{
		"--unit-name", "wordpress/1",
		"thundering typhoons",
	})
	c.Check(err, gc.ErrorMatches, `unrecognized args: \["thundering typhoons"\]`)
}

func waitForUnitActive(stateConn *state.State, unit *state.Unit, c *gc.C) {
	timeout := time.After(5 * time.Second)

	for {
		select {
		case <-timeout:
			c.Fatalf("no activity detected")
		case <-time.After(coretesting.ShortWait):
			err := unit.Refresh()
			c.Assert(err, jc.ErrorIsNil)
			statusInfo, err := unit.Status()
			c.Assert(err, jc.ErrorIsNil)
			switch statusInfo.Status {
			case state.StatusMaintenance, state.StatusWaiting, state.StatusBlocked:
				c.Logf("waiting...")
				continue
			case state.StatusActive:
				c.Logf("active!")
				return
			case state.StatusUnknown:
				// Active units may have a status of unknown if they have
				// started but not run status-set.
				c.Logf("unknown but active!")
				return
			default:
				c.Fatalf("unexpected status %s %s %v", statusInfo.Status, statusInfo.Message, statusInfo.Data)
			}
			statusInfo, err = unit.AgentStatus()
			c.Assert(err, jc.ErrorIsNil)
			switch statusInfo.Status {
			case state.StatusAllocating, state.StatusExecuting, state.StatusRebooting, state.StatusIdle:
				c.Logf("waiting...")
				continue
			case state.StatusError:
				stateConn.StartSync()
				c.Logf("unit is still down")
			default:
				c.Fatalf("unexpected status %s %s %v", statusInfo.Status, statusInfo.Message, statusInfo.Data)
			}
		}
	}
}

func (s *UnitSuite) TestRunStop(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	a := s.newAgent(c, unit)
	go func() { c.Check(a.Run(nil), gc.IsNil) }()
	defer func() { c.Check(a.Stop(), gc.IsNil) }()
	waitForUnitActive(s.State, unit, c)
}

func (s *UnitSuite) TestUpgrade(c *gc.C) {
	machine, unit, _, currentTools := s.primeAgent(c)
	agent := s.newAgent(c, unit)
	newVers := version.Current
	newVers.Patch++
	envtesting.AssertUploadFakeToolsVersions(
		c, s.DefaultToolsStorage, s.Environ.Config().AgentStream(), s.Environ.Config().AgentStream(), newVers)

	// The machine agent downloads the tools; fake this by
	// creating downloaded-tools.txt in data-dir/tools/<version>.
	toolsDir := agenttools.SharedToolsDir(s.DataDir(), newVers)
	err := os.MkdirAll(toolsDir, 0755)
	c.Assert(err, jc.ErrorIsNil)
	toolsPath := filepath.Join(toolsDir, "downloaded-tools.txt")
	testTools := tools.Tools{Version: newVers, URL: "http://testing.invalid/tools"}
	data, err := json.Marshal(testTools)
	c.Assert(err, jc.ErrorIsNil)
	err = ioutil.WriteFile(toolsPath, data, 0644)
	c.Assert(err, jc.ErrorIsNil)

	// Set the machine agent version to trigger an upgrade.
	err = machine.SetAgentVersion(newVers)
	c.Assert(err, jc.ErrorIsNil)
	err = runWithTimeout(agent)
	envtesting.CheckUpgraderReadyError(c, err, &upgrader.UpgradeReadyError{
		AgentName: unit.Tag().String(),
		OldTools:  currentTools.Version,
		NewTools:  newVers,
		DataDir:   s.DataDir(),
	})
}

func (s *UnitSuite) TestUpgradeFailsWithoutTools(c *gc.C) {
	machine, unit, _, _ := s.primeAgent(c)
	agent := s.newAgent(c, unit)
	newVers := version.Current
	newVers.Patch++
	err := machine.SetAgentVersion(newVers)
	c.Assert(err, jc.ErrorIsNil)
	err = runWithTimeout(agent)
	c.Assert(err, gc.ErrorMatches, "timed out waiting for agent to finish.*")
}

func (s *UnitSuite) TestWithDeadUnit(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	err := unit.EnsureDead()
	c.Assert(err, jc.ErrorIsNil)
	a := s.newAgent(c, unit)
	err = runWithTimeout(a)
	c.Assert(err, jc.ErrorIsNil)

	// try again when the unit has been removed.
	err = unit.Remove()
	c.Assert(err, jc.ErrorIsNil)
	a = s.newAgent(c, unit)
	err = runWithTimeout(a)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *UnitSuite) TestOpenAPIState(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	s.RunTestOpenAPIState(c, unit, s.newAgent(c, unit), initialUnitPassword)
}

func (s *UnitSuite) RunTestOpenAPIState(c *gc.C, ent state.AgentEntity, agentCmd Agent, initialPassword string) {
	conf, err := agent.ReadConfig(agent.ConfigPath(s.DataDir(), ent.Tag()))
	c.Assert(err, jc.ErrorIsNil)

	conf.SetPassword("")
	err = conf.Write()
	c.Assert(err, jc.ErrorIsNil)

	// Check that it starts initially and changes the password
	assertOpen := func(conf agent.Config) {
		st, gotEnt, err := OpenAPIState(conf, agentCmd)
		c.Assert(err, jc.ErrorIsNil)
		c.Assert(st, gc.NotNil)
		st.Close()
		c.Assert(gotEnt.Tag(), gc.Equals, ent.Tag().String())
	}
	assertOpen(conf)

	// Check that the initial password is no longer valid.
	err = ent.Refresh()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ent.PasswordValid(initialPassword), jc.IsFalse)

	// Read the configuration and check that we can connect with it.
	conf, err = agent.ReadConfig(agent.ConfigPath(conf.DataDir(), conf.Tag()))
	//conf = refreshConfig(c, conf)
	c.Assert(err, gc.IsNil)
	// Check we can open the API with the new configuration.
	assertOpen(conf)
}

func (s *UnitSuite) TestOpenAPIStateWithBadCredsTerminates(c *gc.C) {
	conf, _ := s.PrimeAgent(c, names.NewUnitTag("missing/0"), "no-password", version.Current)
	_, _, err := OpenAPIState(conf, nil)
	c.Assert(err, gc.Equals, worker.ErrTerminateAgent)
}

type fakeUnitAgent struct {
	unitName string
}

func (f *fakeUnitAgent) Tag() names.Tag {
	return names.NewUnitTag(f.unitName)
}

func (f *fakeUnitAgent) ChangeConfig(agent.ConfigMutator) error {
	panic("fakeUnitAgent.ChangeConfig called unexpectedly")
}

func (s *UnitSuite) TestOpenAPIStateWithDeadEntityTerminates(c *gc.C) {
	_, unit, conf, _ := s.primeAgent(c)
	err := unit.EnsureDead()
	c.Assert(err, jc.ErrorIsNil)
	_, _, err = OpenAPIState(conf, &fakeUnitAgent{"wordpress/0"})
	c.Assert(err, gc.Equals, worker.ErrTerminateAgent)
}

func (s *UnitSuite) TestOpenStateFails(c *gc.C) {
	// Start a unit agent and make sure it doesn't set a mongo password
	// we can use to connect to state with.
	_, unit, conf, _ := s.primeAgent(c)
	a := s.newAgent(c, unit)
	go func() { c.Check(a.Run(nil), gc.IsNil) }()
	defer func() { c.Check(a.Stop(), gc.IsNil) }()
	waitForUnitActive(s.State, unit, c)

	s.AssertCannotOpenState(c, conf.Tag(), conf.DataDir())
}

func (s *UnitSuite) TestRsyslogConfigWorker(c *gc.C) {
	created := make(chan rsyslog.RsyslogMode, 1)
	s.PatchValue(&cmdutil.NewRsyslogConfigWorker, func(_ *apirsyslog.State, _ agent.Config, mode rsyslog.RsyslogMode) (worker.Worker, error) {
		created <- mode
		return newDummyWorker(), nil
	})

	_, unit, _, _ := s.primeAgent(c)
	a := s.newAgent(c, unit)
	go func() { c.Check(a.Run(nil), gc.IsNil) }()
	defer func() { c.Check(a.Stop(), gc.IsNil) }()

	select {
	case <-time.After(coretesting.LongWait):
		c.Fatalf("timeout while waiting for rsyslog worker to be created")
	case mode := <-created:
		c.Assert(mode, gc.Equals, rsyslog.RsyslogModeForwarding)
	}
}

func (s *UnitSuite) TestAgentSetsToolsVersion(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	vers := version.Current
	vers.Minor = version.Current.Minor + 1
	err := unit.SetAgentVersion(vers)
	c.Assert(err, jc.ErrorIsNil)

	a := s.newAgent(c, unit)
	go func() { c.Check(a.Run(nil), gc.IsNil) }()
	defer func() { c.Check(a.Stop(), gc.IsNil) }()

	timeout := time.After(coretesting.LongWait)
	for done := false; !done; {
		select {
		case <-timeout:
			c.Fatalf("timeout while waiting for agent version to be set")
		case <-time.After(coretesting.ShortWait):
			err := unit.Refresh()
			c.Assert(err, jc.ErrorIsNil)
			agentTools, err := unit.AgentTools()
			c.Assert(err, jc.ErrorIsNil)
			if agentTools.Version.Minor != version.Current.Minor {
				continue
			}
			c.Assert(agentTools.Version, gc.DeepEquals, version.Current)
			done = true
		}
	}
}

func (s *UnitSuite) TestUnitAgentRunsAPIAddressUpdaterWorker(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	a := s.newAgent(c, unit)
	go func() { c.Check(a.Run(nil), gc.IsNil) }()
	defer func() { c.Check(a.Stop(), gc.IsNil) }()

	// Update the API addresses.
	updatedServers := [][]network.HostPort{
		network.NewHostPorts(1234, "localhost"),
	}
	err := s.BackingState.SetAPIHostPorts(updatedServers)
	c.Assert(err, jc.ErrorIsNil)

	// Wait for config to be updated.
	s.BackingState.StartSync()
	for attempt := coretesting.LongAttempt.Start(); attempt.Next(); {
		addrs, err := a.CurrentConfig().APIAddresses()
		c.Assert(err, jc.ErrorIsNil)
		if reflect.DeepEqual(addrs, []string{"localhost:1234"}) {
			return
		}
	}
	c.Fatalf("timeout while waiting for agent config to change")
}

func (s *UnitSuite) TestUnitAgentAPIWorkerErrorClosesAPI(c *gc.C) {
	_, unit, _, _ := s.primeAgent(c)
	a := s.newAgent(c, unit)
	a.apiStateUpgrader = &unitAgentUpgrader{}

	closedAPI := make(chan io.Closer, 1)
	s.AgentSuite.PatchValue(&reportClosedUnitAPI, func(st io.Closer) {
		select {
		case closedAPI <- st:
			close(closedAPI)
		default:
		}
	})

	worker, err := a.APIWorkers()

	select {
	case closed := <-closedAPI:
		c.Assert(closed, gc.NotNil)
	case <-time.After(coretesting.LongWait):
		c.Fatalf("API not opened")
	}

	c.Assert(worker, gc.IsNil)
	c.Assert(err, gc.ErrorMatches, "cannot set unit agent version: test failure")
}

type unitAgentUpgrader struct{}

func (u *unitAgentUpgrader) SetVersion(s string, v version.Binary) error {
	return errors.New("test failure")
}

func (s *UnitSuite) TestUseLumberjack(c *gc.C) {
	ctx, err := cmd.DefaultContext()
	c.Assert(err, gc.IsNil)

	a := UnitAgent{
		AgentConf: FakeAgentConfig{},
		ctx:       ctx,
		UnitName:  "mysql/25",
	}

	err = a.Init(nil)
	c.Assert(err, gc.IsNil)

	l, ok := ctx.Stderr.(*lumberjack.Logger)
	c.Assert(ok, jc.IsTrue)
	c.Check(l.MaxAge, gc.Equals, 0)
	c.Check(l.MaxBackups, gc.Equals, 2)
	c.Check(l.Filename, gc.Equals, filepath.FromSlash("/var/log/juju/machine-42.log"))
	c.Check(l.MaxSize, gc.Equals, 300)
}

func (s *UnitSuite) TestDontUseLumberjack(c *gc.C) {
	ctx, err := cmd.DefaultContext()
	c.Assert(err, gc.IsNil)

	a := UnitAgent{
		AgentConf: FakeAgentConfig{},
		ctx:       ctx,
		UnitName:  "mysql/25",

		// this is what would get set by the CLI flags to tell us not to log to
		// the file.
		logToStdErr: true,
	}

	err = a.Init(nil)
	c.Assert(err, gc.IsNil)

	_, ok := ctx.Stderr.(*lumberjack.Logger)
	c.Assert(ok, jc.IsFalse)
}
