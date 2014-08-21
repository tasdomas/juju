// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package firewaller

import (
	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/network"
	"github.com/juju/juju/state/api/base"
	"github.com/juju/juju/state/api/common"
	"github.com/juju/juju/state/api/params"
	"github.com/juju/juju/state/api/watcher"
)

const firewallerFacade = "Firewaller"

// State provides access to the Firewaller API facade.
type State struct {
	facade base.FacadeCaller
	*common.EnvironWatcher
}

// NewState creates a new client-side Firewaller facade.
func NewState(caller base.APICaller) *State {
	facadeCaller := base.NewFacadeCaller(caller, firewallerFacade)
	return &State{
		facade:         facadeCaller,
		EnvironWatcher: common.NewEnvironWatcher(facadeCaller),
	}
}

// life requests the life cycle of the given entity from the server.
func (st *State) life(tag names.Tag) (params.Life, error) {
	return common.Life(st.facade, tag)
}

// Unit provides access to methods of a state.Unit through the facade.
func (st *State) Unit(tag names.UnitTag) (*Unit, error) {
	life, err := st.life(tag)
	if err != nil {
		return nil, err
	}
	return &Unit{
		tag:  tag,
		life: life,
		st:   st,
	}, nil
}

// Machine provides access to methods of a state.Machine through the
// facade.
func (st *State) Machine(tag names.MachineTag) (*Machine, error) {
	life, err := st.life(tag)
	if err != nil {
		return nil, err
	}
	return &Machine{
		tag:  tag,
		life: life,
		st:   st,
	}, nil
}

// WatchEnvironMachines returns a StringsWatcher that notifies of
// changes to the life cycles of the top level machines in the current
// environment.
func (st *State) WatchEnvironMachines() (watcher.StringsWatcher, error) {
	var result params.StringsWatchResult
	err := st.facade.FacadeCall("WatchEnvironMachines", nil, &result)
	if err != nil {
		return nil, err
	}
	if err := result.Error; err != nil {
		return nil, result.Error
	}
	w := watcher.NewStringsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

// WatchOpenedPorts returns a StringsWatcher that notifies of changes
// to the ports open on machines.
func (st *State) WatchOpenedPorts() (watcher.StringsWatcher, error) {
	var result params.StringsWatchResult
	err := st.facade.FacadeCall("WatchOpenedPorts", nil, &result)
	if err != nil {
		return nil, err
	}
	if err := result.Error; err != nil {
		return nil, result.Error
	}
	w := watcher.NewStringsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

// GetMachinePorts returns opened port information for machine entities.
func (st *State) GetMachinePorts(machine names.Tag, net names.Tag) (map[network.PortRange]string, error) {
	var rawResult params.MachinePortsResults
	args := params.MachinePortsParams{
		Params: []params.MachinePortsParam{{Machine: machine.String(), Network: net.String()}},
	}
	if err := st.facade.FacadeCall("GetMachinePorts", args, &rawResult); err != nil {
		return nil, err
	}
	if len(rawResult.Results) != 1 {
		return nil, errors.Errorf("expected 1 result, got %d", len(rawResult.Results))
	}
	if err := rawResult.Results[0].Error; err != nil {
		return nil, err
	}
	result := map[network.PortRange]string{}
	for _, portDef := range rawResult.Results[0].Ports {
		result[portDef.Range] = portDef.Unit.Tag
	}
	return result, nil
}
