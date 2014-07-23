// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package upgrades

import (
	"time"

	"labix.org/v2/mgo/bson"
	"labix.org/v2/mgo/txn"
)

const (
	userCollectionName = "users"
)

type userDocBefore struct {
	Name           string    `bson:"_id"`
	LastConnection time.Time `bson:"lastconnection"`
}

// The LastConnection field on the user document should be renamed to
// LastLogin.
func migrateLastConnectionToLastLogin(context Context) error {
	var oldDocs []userDocBefore

	err := context.State().ResumeTransactions()
	if err != nil {
		return err
	}

	err = context.DB().C(userCollectionName).Find(bson.D{{
		"lastconnection", bson.D{{"$exists", true}}}}).All(&oldDocs)
	if err != nil {
		return err
	}

	var zeroTime time.Time

	ops := []txn.Op{}
	for _, oldDoc := range oldDocs {
		var lastLogin *time.Time
		if oldDoc.LastConnection != zeroTime {
			lastLogin = &oldDoc.LastConnection
		}

		ops = append(ops,
			txn.Op{
				C:      userCollectionName,
				Id:     oldDoc.Name,
				Assert: txn.DocExists,
				Update: bson.D{
					{"$set", bson.D{{"lastlogin", lastLogin}}},
					{"$unset", bson.D{{"lastconnection", nil}}},
				},
			})
	}

	return context.Run(ops)
}
