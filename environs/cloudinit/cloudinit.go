package cloudinit

import (
	"encoding/base64"
	"fmt"
	"launchpad.net/goyaml"
	"launchpad.net/juju-core/cloudinit"
	"launchpad.net/juju-core/environs"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/log"
	"launchpad.net/juju-core/state"
	"launchpad.net/juju-core/trivial"
	"launchpad.net/juju-core/upstart"
	"path"
	"strings"
)

// TODO(dfc) duplicated from environs/ec2

const mgoPort = 37017

var mgoPortSuffix = fmt.Sprintf(":%d", mgoPort)

// MachineConfig represents initialization information for a new juju machine.
// Creation of cloudinit data from this struct is largely provider-independent,
// but we'll keep it internal until we need to factor it out.
type MachineConfig struct {
	// StateServer specifies whether the new machine will run a ZooKeeper
	// or MongoDB instance.
	StateServer bool

	// StateServerCertPEM and StateServerKeyPEM hold the state server
	// certificate and private key in PEM format; they are required when
	// StateServer is set, and ignored otherwise.
	StateServerCertPEM, StateServerKeyPEM []byte

	// InstanceIdAccessor holds bash code that evaluates to the current instance id.
	InstanceIdAccessor string

	// ProviderType identifies the provider type so the host
	// knows which kind of provider to use.
	ProviderType string

	// StateInfo holds the means for the new instance to communicate with the
	// juju state. Unless the new machine is running a state server (StateServer is
	// set), there must be at least one state server address supplied.
	// The entity name must match that of the machine being started,
	// or be empty when starting a state server.
	StateInfo *state.Info

	// Tools is juju tools to be used on the new machine.
	Tools *state.Tools

	// DataDir holds the directory that juju state will be put in the new
	// machine.
	DataDir string

	// MachineId identifies the new machine. It must be non-negative.
	MachineId int

	// AuthorizedKeys specifies the keys that are allowed to
	// connect to the machine (see cloudinit.SSHAddAuthorizedKeys)
	// If no keys are supplied, there can be no ssh access to the node.
	// On a bootstrap machine, that is fatal. On other
	// machines it will mean that the ssh, scp and debug-hooks
	// commands cannot work.
	AuthorizedKeys string

	// Config holds the initial environment configuration.
	Config *config.Config
}

func addScripts(c *cloudinit.Config, scripts ...string) {
	for _, s := range scripts {
		c.AddRunCmd(s)
	}
}

func base64yaml(m *config.Config) string {
	data, err := goyaml.Marshal(m.AllAttrs())
	if err != nil {
		// can't happen, these values have been validated a number of times
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

const serverPEMPath = "/var/lib/juju/server.pem"

func New(cfg *MachineConfig) (*cloudinit.Config, error) {
	if err := verifyConfig(cfg); err != nil {
		return nil, err
	}
	c := cloudinit.New()

	c.AddSSHAuthorizedKeys(cfg.AuthorizedKeys)
	c.AddPackage("git")

	addScripts(c,
		fmt.Sprintf("mkdir -p %s", cfg.DataDir),
		"mkdir -p /var/log/juju")

	// Make a directory for the tools to live in, then fetch the
	// tools and unarchive them into it.
	addScripts(c,
		"bin="+shquote(cfg.jujuTools()),
		"mkdir -p $bin",
		fmt.Sprintf("wget --no-verbose -O - %s | tar xz -C $bin", shquote(cfg.Tools.URL)),
		fmt.Sprintf("echo -n %s > $bin/downloaded-url.txt", shquote(cfg.Tools.URL)),
	)

	debugFlag := ""
	// TODO: disable debug mode by default when the system is stable.
	if true || log.Debug {
		debugFlag = " --debug"
	}

	if cfg.StateServer {
		addScripts(c,
			fmt.Sprintf("echo %s > %s",
				shquote(string(cfg.StateServerCertPEM) + string(cfg.StateServerKeyPEM)), serverPEMPath),
			"chmod 600 "+serverPEMPath,
		)

		// TODO The public bucket must come from the environment configuration.
		b := cfg.Tools.Binary
		url := fmt.Sprintf("http://juju-dist.s3.amazonaws.com/tools/mongo-2.2.0-%s-%s.tgz", b.Series, b.Arch)
		addScripts(c,
			"mkdir -p /opt",
			fmt.Sprintf("wget --no-verbose -O - %s | tar xz -C /opt", shquote(url)),
		)
		if err := addMongoToBoot(c); err != nil {
			return nil, err
		}
		addScripts(c, cfg.jujuTools()+"/jujud bootstrap-state"+
			" --instance-id "+cfg.InstanceIdAccessor+
			" --env-config "+shquote(base64yaml(cfg.Config))+
			" --state-servers localhost"+mgoPortSuffix+
			" --initial-password "+shquote(cfg.StateInfo.Password)+
			debugFlag,
		)

	}

	if err := addAgentToBoot(c, cfg, "machine",
		state.MachineEntityName(cfg.MachineId),
		fmt.Sprintf("--machine-id %d "+debugFlag, cfg.MachineId)); err != nil {
		return nil, err
	}

	// general options
	c.SetAptUpgrade(true)
	c.SetAptUpdate(true)
	c.SetOutput(cloudinit.OutAll, "| tee -a /var/log/cloud-init-output.log", "")
	return c, nil
}

func addAgentToBoot(c *cloudinit.Config, cfg *MachineConfig, kind, name, args string) error {
	// Make the agent run via a symbolic link to the actual tools
	// directory, so it can upgrade itself without needing to change
	// the upstart script.
	toolsDir := environs.AgentToolsDir(cfg.DataDir, name)
	// TODO(dfc) ln -nfs, so it doesn't fail if for some reason that the target already exists
	addScripts(c, fmt.Sprintf("ln -s %v %s", cfg.Tools.Binary, shquote(toolsDir)))

	agentDir := environs.AgentDir(cfg.DataDir, name)
	addScripts(c, fmt.Sprintf("mkdir -p %s", shquote(agentDir)))
	svc := upstart.NewService("jujud-" + name)
	logPath := fmt.Sprintf("/var/log/juju/%s.log", name)
	cmd := fmt.Sprintf(
		"%s/jujud %s"+
			" --state-servers '%s'"+
			" --log-file %s"+
			" --data-dir '%s'"+
			" --initial-password '%s'"+
			" %s",
		toolsDir, kind,
		cfg.stateHostAddrs(),
		logPath,
		cfg.DataDir,
		cfg.StateInfo.Password,
		args,
	)
	conf := &upstart.Conf{
		Service: *svc,
		Desc:    fmt.Sprintf("juju %s agent", name),
		Cmd:     cmd,
		Out:     logPath,
	}
	cmds, err := conf.InstallCommands()
	if err != nil {
		return fmt.Errorf("cannot make cloud-init upstart script for the %s agent: %v", name, err)
	}
	addScripts(c, cmds...)
	return nil
}

func addMongoToBoot(c *cloudinit.Config) error {
	addScripts(c,
		"mkdir -p /var/lib/juju/db/journal",
		// Otherwise we get three files with 100M+ each, which takes time.
		"dd bs=1M count=1 if=/dev/zero of=/var/lib/juju/db/journal/prealloc.0",
		"dd bs=1M count=1 if=/dev/zero of=/var/lib/juju/db/journal/prealloc.1",
		"dd bs=1M count=1 if=/dev/zero of=/var/lib/juju/db/journal/prealloc.2",
	)
	svc := upstart.NewService("juju-db")
	conf := &upstart.Conf{
		Service: *svc,
		Desc:    "juju state database",
		Cmd: "/opt/mongo/bin/mongod" +
			" --auth" +
			" --dbpath=/var/lib/juju/db" +
			" --bind_ip 0.0.0.0" +
			" --port " + fmt.Sprint(mgoPort) +
			" --noprealloc" +
			" --smallfiles",
	}
	cmds, err := conf.InstallCommands()
	if err != nil {
		return fmt.Errorf("cannot make cloud-init upstart script for the state database: %v", err)
	}
	addScripts(c, cmds...)
	return nil
}

// versionDir converts a tools URL into a name
// to use as a directory for storing the tools executables in
// by using the last element stripped of its extension.
func versionDir(toolsURL string) string {
	name := path.Base(toolsURL)
	ext := path.Ext(name)
	return name[:len(name)-len(ext)]
}

func (cfg *MachineConfig) jujuTools() string {
	return environs.ToolsDir(cfg.DataDir, cfg.Tools.Binary)
}

func (cfg *MachineConfig) stateHostAddrs() string {
	var hosts []string
	if cfg.StateServer {
		hosts = append(hosts, "localhost"+mgoPortSuffix)
	}
	if cfg.StateInfo != nil {
		hosts = append(hosts, cfg.StateInfo.Addrs...)
	}
	return strings.Join(hosts, ",")
}

// shquote quotes s so that when read by bash, no metacharacters
// within s will be interpreted as such.
func shquote(s string) string {
	// single-quote becomes single-quote, double-quote, single-quote, double-quote, single-quote
	return `'` + strings.Replace(s, `'`, `'"'"'`, -1) + `'`
}

type requiresError string

func (e requiresError) Error() string {
	return "invalid machine configuration: missing " + string(e)
}

func verifyConfig(cfg *MachineConfig) (err error) {
	defer trivial.ErrorContextf(&err, "invalid machine configuration")
	if cfg.MachineId < 0 {
		return fmt.Errorf("negative machine id")
	}
	if cfg.ProviderType == "" {
		return fmt.Errorf("missing provider type")
	}
	if cfg.DataDir == "" {
		return fmt.Errorf("missing var directory")
	}
	if cfg.Tools == nil {
		return fmt.Errorf("missing tools")
	}
	if cfg.Tools.URL == "" {
		return fmt.Errorf("missing tools URL")
	}
	if cfg.StateInfo == nil {
		return fmt.Errorf("missing state info")
	}
	if cfg.StateServer {
		if cfg.InstanceIdAccessor == "" {
			return fmt.Errorf("missing instance id accessor")
		}
		if cfg.Config == nil {
			return fmt.Errorf("missing environment configuration")
		}
		if cfg.StateInfo.EntityName != "" {
			return fmt.Errorf("entity name must be blank when starting a state server")
		}
		if len(cfg.StateServerCertPEM) == 0 {
			return fmt.Errorf("missing state server certificate")
		}
		if len(cfg.StateServerKeyPEM) == 0 {
			return fmt.Errorf("missing state server private key")
		}
	} else {
		if len(cfg.StateInfo.Addrs) == 0 {
			return fmt.Errorf("missing state hosts")
		}
		if cfg.StateInfo.EntityName != state.MachineEntityName(cfg.MachineId) {
			return fmt.Errorf("entity name must match started machine")
		}
	}
	for _, r := range cfg.StateInfo.Password {
		if r == '\'' || r == '\\' || r < 32 {
			return fmt.Errorf("password has disallowed characters")
		}
	}
	return nil
}
