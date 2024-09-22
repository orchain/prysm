package checkpointsync

import (
	"context"
	"fmt"
	"github.com/prysmaticlabs/prysm/v4/cmd/flags"
	"github.com/prysmaticlabs/prysm/v4/config/params"
	"github.com/prysmaticlabs/prysm/v4/runtime/version"
	"os"
	"strings"
	"time"

	"github.com/prysmaticlabs/prysm/v4/api/client"
	"github.com/prysmaticlabs/prysm/v4/api/client/beacon"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var downloadFlags = struct {
	BeaconNodeHost string
	Timeout        time.Duration
}{}
var generateGenesisStateFlags = struct {
	DepositJsonFile    string
	ChainConfigFile    string
	ConfigName         string
	NumValidators      uint64
	GenesisTime        uint64
	GenesisTimeDelay   uint64
	OutputSSZ          string
	OutputJSON         string
	OutputYaml         string
	ForkName           string
	OverrideEth1Data   bool
	ExecutionEndpoint  string
	GethGenesisJsonIn  string
	GethGenesisJsonOut string
}{}
var downloadCmd = &cli.Command{
	Name:    "download",
	Aliases: []string{"dl"},
	Usage:   "Download the latest finalized state and the most recent block it integrates. To be used for checkpoint sync.",
	Action: func(cliCtx *cli.Context) error {
		if err := cliActionDownload(cliCtx); err != nil {
			log.WithError(err).Fatal("Could not download checkpoint-sync data")
		}
		return nil
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "beacon-node-host",
			Usage:       "host:port for beacon node connection",
			Destination: &downloadFlags.BeaconNodeHost,
			Value:       "localhost:3500",
		},
		&cli.DurationFlag{
			Name:        "http-timeout",
			Usage:       "timeout for http requests made to beacon-node-url (uses duration format, ex: 2m31s). default: 4m",
			Destination: &downloadFlags.Timeout,
			Value:       time.Minute * 4,
		},
		&cli.StringFlag{
			Name:        "chain-config-file",
			Destination: &generateGenesisStateFlags.ChainConfigFile,
			Usage:       "The path to a YAML file with chain config values",
		},
		&cli.StringFlag{
			Name:        "config-name",
			Usage:       "Config kind to be used for generating the genesis state. Default: mainnet. Options include mainnet, interop, minimal, prater, sepolia. --chain-config-file will override this flag.",
			Destination: &generateGenesisStateFlags.ConfigName,
			Value:       params.MainnetName,
		},
		flags.EnumValue{
			Name:        "fork",
			Usage:       fmt.Sprintf("Name of the BeaconState schema to use in output encoding [%s]", strings.Join(versionNames(), ",")),
			Enum:        versionNames(),
			Value:       versionNames()[0],
			Destination: &generateGenesisStateFlags.ForkName,
		}.GenericFlag(),
	},
}

func versionNames() []string {
	enum := version.All()
	names := make([]string, len(enum))
	for i := range enum {
		names[i] = version.String(enum[i])
	}
	return names
}

func setGlobalParams() error {
	chainConfigFile := generateGenesisStateFlags.ChainConfigFile
	if chainConfigFile != "" {
		log.Infof("Specified a chain config file: %s", chainConfigFile)
		return params.LoadChainConfigFile(chainConfigFile, nil)
	}
	cfg, err := params.ByName(generateGenesisStateFlags.ConfigName)
	if err != nil {
		return fmt.Errorf("unable to find config using name %s: %v", generateGenesisStateFlags.ConfigName, err)
	}
	return params.SetActive(cfg.Copy())
}

func cliActionDownload(_ *cli.Context) error {
	ctx := context.Background()
	f := downloadFlags
	setGlobalParams()
	opts := []client.ClientOpt{client.WithTimeout(f.Timeout)}
	client, err := beacon.NewClient(downloadFlags.BeaconNodeHost, opts...)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	od, err := beacon.DownloadFinalizedData(ctx, client)
	if err != nil {
		return err
	}

	blockPath, err := od.SaveBlock(cwd)
	if err != nil {
		return err
	}
	log.Printf("saved ssz-encoded block to %s", blockPath)

	statePath, err := od.SaveState(cwd)
	if err != nil {
		return err
	}
	log.Printf("saved ssz-encoded state to %s", statePath)

	return nil
}
