// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"time"

	"labix.org/v2/mgo/bson"
	"labix.org/v2/mgo/txn"
	gc "launchpad.net/gocheck"

	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/upgrades"
)

type userLastLoginSuite struct {
	jujutesting.JujuConnSuite
	ctx upgrades.Context
}

var _ = gc.Suite(&userLastLoginSuite{})

func (s *userLastLoginSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
}

func (s *userLastLoginSuite) TestLastLoginMigrate(c *gc.C) {
	now := time.Now().UTC().Round(time.Second)
	userId := "foobar"
	oldDoc := bson.M{
		"_id":            userId,
		"displayname":    "foo bar",
		"deactivated":    false,
		"passwordhash":   "hash",
		"passwordsalt":   "salt",
		"createdby":      "creator",
		"datecreated":    now,
		"lastconnection": now,
	}

	ops := []txn.Op{
		txn.Op{
			C:      "users",
			Id:     userId,
			Assert: txn.DocMissing,
			Insert: oldDoc,
		},
	}
	err := s.State.runTransaction(ops)
	c.Assert(err, gc.IsNil)

	err = upgrades.MigrateLastConnectionToLastLogin(s.State)
	c.Assert(err, gc.IsNil)
	user, err := s.State.User(userId)
	c.Assert(err, gc.IsNil)
	c.Assert(*user.LastLogin(), gc.Equals, now)
}
