// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"launchpad.net/gnuflag"

	"launchpad.net/juju-core/cmd"
	"launchpad.net/juju-core/juju"
	"launchpad.net/juju-core/names"
	"launchpad.net/juju-core/state/api"
)

// RunCommand is responsible for running arbitrary commands on remote machines.
type RunCommand struct {
	cmd.EnvCommandBase
	all      bool
	timeout  time.Duration
	machines []string
	services []string
	units    []string
	commands string
}

const runDoc = `
Run the commands on the specified targets.

Targets are specified using either machine ids, service names or unit
names.  At least one target specifier is needed.

Multiple values can be set for --machine, --service, and --unit by using
comma separated values.

If the target is a machine, the command is run as the "ubuntu" user on
the remote machine.

If the target is a service, the command is run on all units for that
service. For example, if there was a service "mysql" and that service
had two units, "mysql/0" and "mysql/1", then
  --service mysql
is equivalent to
  --unit mysql/0,mysql/1

Commands run for services or units are executed in a 'hook context' for
the unit.

--all is provided as a simple way to run the command on all the machines
in the environment.  If you specify --all you cannot provide additional
targets.

`

func (c *RunCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "run",
		Args:    "<commands>",
		Purpose: "run the commands on the remote targets specified",
		Doc:     runDoc,
	}
}

func (c *RunCommand) SetFlags(f *gnuflag.FlagSet) {
	c.EnvCommandBase.SetFlags(f)
	f.BoolVar(&c.all, "all", false, "run the commands on all the machines")
	f.DurationVar(&c.timeout, "timeout", 5*time.Minute, "how long to wait before the remote command is considered to have failed")
	f.Var(cmd.NewStringsValue(nil, &c.machines), "machine", "one or more machine ids")
	f.Var(cmd.NewStringsValue(nil, &c.services), "service", "one or more service names")
	f.Var(cmd.NewStringsValue(nil, &c.units), "unit", "one or more unit ids")
}

func (c *RunCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.New("no commands specified")
	}
	c.commands, args = args[0], args[1:]

	if c.all {
		if len(c.machines) != 0 {
			return fmt.Errorf("You cannot specify --all and individual machines")
		}
		if len(c.services) != 0 {
			return fmt.Errorf("You cannot specify --all and individual services")
		}
		if len(c.units) != 0 {
			return fmt.Errorf("You cannot specify --all and individual units")
		}
	} else {
		if len(c.machines) == 0 && len(c.services) == 0 && len(c.units) == 0 {
			return fmt.Errorf("You must specify a target, either through --all, --machine, --service or --unit")
		}
	}

	var nameErrors []string
	for _, machineId := range c.machines {
		if !names.IsMachine(machineId) {
			nameErrors = append(nameErrors, fmt.Sprintf("  %q is not a valid machine id", machineId))
		}
	}
	for _, service := range c.services {
		if !names.IsService(service) {
			nameErrors = append(nameErrors, fmt.Sprintf("  %q is not a valid service name", service))
		}
	}
	for _, unit := range c.units {
		if !names.IsUnit(unit) {
			nameErrors = append(nameErrors, fmt.Sprintf("  %q is not a valid unit name", unit))
		}
	}
	if len(nameErrors) > 0 {
		return fmt.Errorf("The following run targets are not valid:\n%s",
			strings.Join(nameErrors, "\n"))
	}

	return cmd.CheckEmpty(args)
}

func (c *RunCommand) Run(ctx *cmd.Context) error {
	client, err := getAPIClient(c.EnvName)
	if err != nil {
		return err
	}
	defer client.Close()

	var runResults []api.RunResult
	if c.all {
		runResults, err = client.RunOnAllMachines(c.commands, c.timeout)
	} else {
		params := api.RunParams{
			Commands: c.commands,
			Timeout:  c.timeout,
			Machines: c.machines,
			Services: c.services,
			Units:    c.units,
		}
		runResults, err = client.Run(params)
	}

	if err != nil {
		return err
	}

	// Write results
	fmt.Fprintf(ctx.Stdout, "TODO: write out the results\n")
	_ = runResults
	return nil
}

// In order to be able to easily mock out the API side for testing,
// the API client is got using a function.

type RunClient interface {
	Close() error
	RunOnAllMachines(commands string, timeout time.Duration) ([]api.RunResult, error)
	Run(params api.RunParams) ([]api.RunResult, error)
}

// Here we need the signature to be correct for the interface.
var getAPIClient = func(name string) (RunClient, error) {
	return juju.NewAPIClientFromName(name)
}
