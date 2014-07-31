// Copyright 2014 Canonical Ltd. All rights reserved.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/api/params"
	"github.com/juju/juju/state/watcher"
)

// OpenedPortsWatcher implements a method WatchOpenedPorts
// that can be used by various facades.
type OpenedPortsWatcher struct {
	st          state.PortsWatcher
	resources   *Resources
	getCanWatch GetAuthFunc
}

// NewOpenedPortsWatcher returns a new OpenedPortsWatcher.
func NewOpenedPortsWatcher(st state.PortsWatcher, resources *Resources, getCanWatch GetAuthFunc) *OpenedPortsWatcher {
	return &OpenedPortsWatcher{
		st:          st,
		resources:   resources,
		getCanWatch: getCanWatch,
	}
}

// WatchOpenedPorts returns a StringsWatcher that observes the changes in
// the openedPorts configuration.
func (o *OpenedPortsWatcher) WatchOpenedPorts() (params.StringsWatchResult, error) {
	nothing := params.StringsWatchResult{}

	canWatch, err := o.getCanWatch()
	if err != nil {
		return nothing, err
	}
	// Using empty string for the id of the current
	// environment.
	if !canWatch("") {
		return nothing, ErrPerm
	}

	watch := o.st.WatchOpenedPorts()
	if changes, ok := <-watch.Changes(); ok {
		return params.StringsWatchResult{
			StringsWatcherId: o.resources.Register(watch),
			Changes:          changes,
		}, nil
	}
	return nothing, watcher.MustErr(watch)
}
