package config

import (
	"os"
	"strings"

	"github.com/lightninglabs/lndclient"
)

type AppConfig struct {
	Network     lndclient.Network
	MyNodeID    string
	TLSPath     string
	MacaroonDir string
	Ws          string
}

var Config AppConfig

func InitConfig() {
	if strings.ToUpper(os.Getenv("NETWORK")) != "REGTEST" {
		Config.Network = lndclient.NetworkMainnet
		Config.MyNodeID = "03ef68ccd4b33ae540aea5bf91fcfc70137e031e0cf3823a958c3c3d69239eb7cd"
		Config.TLSPath = "/home/walter/.lnd/tls.cert"
		Config.MacaroonDir = "/home/walter/.lnd/data/chain/bitcoin/mainnet"
		Config.Ws = "wss"
	} else {
		Config.Network = lndclient.NetworkRegtest
		Config.MyNodeID = "0311ca3b2af91d093795d560612d5b9fdf2d38b44db7318ba99e213e19a63b3708"
		Config.TLSPath = "regtest/mounts/regtest/alice/tls.cert"
		Config.MacaroonDir = "regtest/mounts/regtest/alice/"
		Config.Ws = "ws"
	}
}
