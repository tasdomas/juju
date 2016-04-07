// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package featuretests

import (
	"bytes"
	"time"

	"github.com/juju/errors"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/modelmanager"
	"github.com/juju/juju/apiserver/params"
	jujunames "github.com/juju/juju/juju/names"
	"github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/testing/factory"
)

type apiEnvironmentSuite struct {
	testing.JujuConnSuite
	client *api.Client
}

func (s *apiEnvironmentSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
	s.client = s.APIState.Client()
	c.Assert(s.client, gc.NotNil)
	s.AddCleanup(func(*gc.C) {
		s.client.ClientFacade.Close()
	})
}

func (s *apiEnvironmentSuite) TestGrantModel(c *gc.C) {
	username := "foo@ubuntuone"
	model, err := s.State.Model()
	c.Assert(err, jc.ErrorIsNil)
	mm := modelmanager.NewClient(s.APIState)
	err = mm.GrantModel(username, "read", model.UUID())
	c.Assert(err, jc.ErrorIsNil)

	user := names.NewUserTag(username)
	modelUser, err := s.State.ModelUser(user)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(modelUser.UserName(), gc.Equals, user.Canonical())
	lastConn, err := modelUser.LastConnection()
	c.Assert(err, jc.Satisfies, state.IsNeverConnectedError)
	c.Assert(lastConn.IsZero(), jc.IsTrue)
}

func (s *apiEnvironmentSuite) TestRevokeModel(c *gc.C) {
	// First share an environment with a user.
	user := s.Factory.MakeModelUser(c, &factory.ModelUserParams{User: "foo@ubuntuone"})
	model, err := s.State.Model()
	c.Assert(err, jc.ErrorIsNil)
	mm := modelmanager.NewClient(s.APIState)

	modelUser, err := s.State.ModelUser(user.UserTag())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(modelUser, gc.NotNil)

	// Then test unsharing the environment.
	err = mm.RevokeModel(user.UserName(), "read", model.UUID())
	c.Assert(err, jc.ErrorIsNil)

	modelUser, err = s.State.ModelUser(user.UserTag())
	c.Assert(errors.IsNotFound(err), jc.IsTrue)
	c.Assert(modelUser, gc.IsNil)
}

func (s *apiEnvironmentSuite) TestEnvironmentUserInfo(c *gc.C) {
	modelUser := s.Factory.MakeModelUser(c, &factory.ModelUserParams{User: "bobjohns@ubuntuone", DisplayName: "Bob Johns"})
	env, err := s.State.Model()
	c.Assert(err, jc.ErrorIsNil)
	owner, err := s.State.ModelUser(env.Owner())
	c.Assert(err, jc.ErrorIsNil)

	obtained, err := s.client.ModelUserInfo()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(obtained, gc.DeepEquals, []params.ModelUserInfo{
		{
			UserName:       owner.UserName(),
			DisplayName:    owner.DisplayName(),
			Access:         "write",
			LastConnection: lastConnPointer(c, owner),
		}, {
			UserName:       "bobjohns@ubuntuone",
			DisplayName:    "Bob Johns",
			Access:         "write",
			LastConnection: lastConnPointer(c, modelUser),
		},
	})
}

func lastConnPointer(c *gc.C, modelUser *state.ModelUser) *time.Time {
	lastConn, err := modelUser.LastConnection()
	if err != nil {
		if state.IsNeverConnectedError(err) {
			return nil
		}
		c.Fatal(err)
	}
	return &lastConn
}

func (s *apiEnvironmentSuite) TestUploadToolsOtherEnvironment(c *gc.C) {
	// setup other environment
	otherState := s.Factory.MakeModel(c, nil)
	defer otherState.Close()
	info := s.APIInfo(c)
	info.ModelTag = otherState.ModelTag()
	otherAPIState, err := api.Open(info, api.DefaultDialOpts())
	c.Assert(err, jc.ErrorIsNil)
	defer otherAPIState.Close()
	otherClient := otherAPIState.Client()
	defer otherClient.ClientFacade.Close()

	newVersion := version.MustParseBinary("5.4.3-quantal-amd64")
	vers := newVersion.String()

	// build fake tools
	tgz, checksum := coretesting.TarGz(
		coretesting.NewTarFile(jujunames.Jujud, 0777, "jujud contents "+vers))

	tool, err := otherClient.UploadTools(bytes.NewReader(tgz), newVersion)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(tool.SHA256, gc.Equals, checksum)

	toolStrg, err := otherState.ToolsStorage()
	defer toolStrg.Close()
	c.Assert(err, jc.ErrorIsNil)
	meta, closer, err := toolStrg.Open(vers)
	defer closer.Close()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(meta.SHA256, gc.Equals, checksum)
	c.Assert(meta.Version, gc.Equals, vers)
}
