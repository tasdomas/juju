// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package leadership_test

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/state/leadership"
	"github.com/juju/juju/state/lease"
	coretesting "github.com/juju/juju/testing"
)

type ExpireLeadershipSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&ExpireLeadershipSuite{})

func (s *ExpireLeadershipSuite) TestStartup_ExpiryInPast(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(-time.Second)},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, _ *coretesting.Clock) {})
}

func (s *ExpireLeadershipSuite) TestStartup_ExpiryInFuture(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(time.Second)},
		},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(almostSeconds(1))
	})
}

func (s *ExpireLeadershipSuite) TestStartup_ExpiryInFuture_TimePasses(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(time.Second)},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(time.Second)
	})
}

func (s *ExpireLeadershipSuite) TestStartup_NoExpiry_NotLongEnough(c *gc.C) {
	fix := &Fixture{}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(almostSeconds(3600))
	})
}

func (s *ExpireLeadershipSuite) TestStartup_NoExpiry_LongEnough(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"goose": lease.Info{Expiry: offset(3 * time.Hour)},
		},
		expectCalls: []call{{
			method: "Refresh",
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{
					Expiry: offset(time.Minute),
				}
			},
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(time.Hour)
	})
}

func (s *ExpireLeadershipSuite) TestExpire_ErrInvalid_Expired(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(time.Second)},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			err:    lease.ErrInvalid,
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(time.Second)
	})
}

func (s *ExpireLeadershipSuite) TestExpire_ErrInvalid_Updated(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(time.Second)},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			err:    lease.ErrInvalid,
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{Expiry: offset(time.Minute)}
			},
		}},
	}
	fix.RunTest(c, func(_ leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(time.Second)
	})
}

func (s *ExpireLeadershipSuite) TestExpire_OtherError(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{Expiry: offset(time.Second)},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			err:    errors.New("snarfblat hobalob"),
		}},
		expectDirty: true,
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		clock.Advance(time.Second)
		err := manager.Wait()
		c.Check(err, gc.ErrorMatches, "snarfblat hobalob")
	})
}

func (s *ExpireLeadershipSuite) TestClaim_ExpiryInFuture(c *gc.C) {
	fix := &Fixture{
		expectCalls: []call{{
			method: "ClaimLease",
			args:   []interface{}{"redis", lease.Request{"redis/0", time.Minute}},
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{
					Holder: "redis/0",
					Expiry: offset(63 * time.Second),
				}
			},
		}},
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		// Ask for a minute, actually get 63s. Don't expire early.
		err := manager.ClaimLeadership("redis", "redis/0", time.Minute)
		c.Assert(err, jc.ErrorIsNil)

		waitAlarms(c, clock, 1)
		clock.Advance(almostSeconds(63))
	})
}

func (s *ExpireLeadershipSuite) TestClaim_ExpiryInFuture_TimePasses(c *gc.C) {
	fix := &Fixture{
		expectCalls: []call{{
			method: "ClaimLease",
			args:   []interface{}{"redis", lease.Request{"redis/0", time.Minute}},
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{
					Holder: "redis/0",
					Expiry: offset(63 * time.Second),
				}
			},
		}, {
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		// Ask for a minute, actually get 63s. Expire on time.
		err := manager.ClaimLeadership("redis", "redis/0", time.Minute)
		c.Assert(err, jc.ErrorIsNil)

		waitAlarms(c, clock, 1)
		clock.Advance(63 * time.Second)
	})
}

func (s *ExpireLeadershipSuite) TestExtend_ExpiryInFuture(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{
				Holder: "redis/0",
				Expiry: offset(time.Second),
			},
		},
		expectCalls: []call{{
			method: "ExtendLease",
			args:   []interface{}{"redis", lease.Request{"redis/0", time.Minute}},
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{
					Holder: "redis/0",
					Expiry: offset(63 * time.Second),
				}
			},
		}},
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		// Ask for a minute, actually get 63s. Don't expire early.
		err := manager.ClaimLeadership("redis", "redis/0", time.Minute)
		c.Assert(err, jc.ErrorIsNil)

		waitAlarms(c, clock, 1)
		clock.Advance(almostSeconds(63))
	})
}

func (s *ExpireLeadershipSuite) TestExtend_ExpiryInFuture_TimePasses(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{
				Holder: "redis/0",
				Expiry: offset(time.Second),
			},
		},
		expectCalls: []call{{
			method: "ExtendLease",
			args:   []interface{}{"redis", lease.Request{"redis/0", time.Minute}},
			callback: func(leases map[string]lease.Info) {
				leases["redis"] = lease.Info{
					Holder: "redis/0",
					Expiry: offset(63 * time.Second),
				}
			},
		}, {
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}},
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		// Ask for a minute, actually get 63s. Expire on time.
		err := manager.ClaimLeadership("redis", "redis/0", time.Minute)
		c.Assert(err, jc.ErrorIsNil)

		waitAlarms(c, clock, 1)
		clock.Advance(63 * time.Second)
	})
}

func (s *ExpireLeadershipSuite) TestExpire_Multiple(c *gc.C) {
	fix := &Fixture{
		leases: map[string]lease.Info{
			"redis": lease.Info{
				Holder: "redis/0",
				Expiry: offset(time.Second),
			},
			"store": lease.Info{
				Holder: "store/3",
				Expiry: offset(5 * time.Second),
			},
			"tokumx": lease.Info{
				Holder: "tokumx/5",
				Expiry: offset(10 * time.Second), // will not expire.
			},
			"ultron": lease.Info{
				Holder: "ultron/7",
				Expiry: offset(5 * time.Second),
			},
			"vvvvvv": lease.Info{
				Holder: "vvvvvv/2",
				Expiry: offset(time.Second), // would expire, but errors first.
			},
			"worgen": lease.Info{
				Holder: "worgen/2",
				Expiry: offset(time.Minute), // verifies absence of loop-var bug.
			},
		},
		expectCalls: []call{{
			method: "Refresh",
		}, {
			method: "ExpireLease",
			args:   []interface{}{"redis"},
			callback: func(leases map[string]lease.Info) {
				delete(leases, "redis")
			},
		}, {
			method: "ExpireLease",
			args:   []interface{}{"store"},
			err:    lease.ErrInvalid,
			callback: func(leases map[string]lease.Info) {
				delete(leases, "store")
			},
		}, {
			method: "ExpireLease",
			args:   []interface{}{"ultron"},
			err:    errors.New("what is this?"),
		}},
		expectDirty: true,
	}
	fix.RunTest(c, func(manager leadership.ManagerWorker, clock *coretesting.Clock) {
		wait := make(chan error)
		go func() {
			wait <- manager.Wait()
		}()

		clock.Advance(5 * time.Second)
		select {
		case err := <-wait:
			c.Check(err, gc.ErrorMatches, "what is this\\?")
		case <-time.After(coretesting.LongWait):
			c.Fatalf("manager never stopped")
		}
	})
}
