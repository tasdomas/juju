// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package testing

import (
	"time"

	"github.com/juju/errors"
	coretesting "github.com/juju/juju/testing"
)

// mockTimer implements the worker.Timer interface.
type mockTimer struct {
	Period chan time.Duration
	c      chan time.Time
}

func (t *mockTimer) Reset(d time.Duration) bool {
	select {
	case t.Period <- d:
	case <-time.After(coretesting.LongWait):
		panic("timed out waiting for timer to reset")
	}
	return true
}

func (t *mockTimer) CountDown() <-chan time.Time {
	return t.c
}

func (t *mockTimer) Fire() error {
	select {
	case t.c <- time.Time{}:
	case <-time.After(coretesting.LongWait):
		return errors.New("timed out waiting for pruner to run")
	}
	return nil
}

// NewMockTimer creates a new mockTimer.
func NewMockTimer(d time.Duration) *mockTimer {
	return &mockTimer{Period: make(chan time.Duration, 1),
		c: make(chan time.Time),
	}
}
