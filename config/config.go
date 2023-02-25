package config

import "github.com/lightninglabs/lndclient"

type AppConfig struct {
	Network     lndclient.Network
	MyNodeID    string
	TLSPath     string
	MacaroonDir string
}

var Config AppConfig

func InitConfig() {
	Config.Network = lndclient.NetworkMainnet
	Config.MyNodeID = "03ef68ccd4b33ae540aea5bf91fcfc70137e031e0cf3823a958c3c3d69239eb7cd"
	Config.TLSPath = "/home/walter/.lnd/tls.cert"
	Config.MacaroonDir = "/home/walter/.lnd/data/chain/bitcoin/mainnet"
}
