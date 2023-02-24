#!/bin/bash

DIR="/home/walter/Development/lightning-lotto/regtest"
COMPOSE="docker-compose -f $DIR/regtest.yml -p regtest"
DATA_DIR=$DIR/mounts/regtest

function reg_bitcoin() {
  docker exec -u bitcoin "regtest-bitcoind-1" bitcoin-cli -regtest -rpcuser=lightning -rpcpassword=lightning "$@"
}

function start() {
  $COMPOSE up --force-recreate -d
  echo "waiting for nodes to start"
  waitnodestart
  copymacaroons
  setup
}

function setup() {
  echo "creating wallet"
  reg_bitcoin createwallet miner

  echo "Getting pubkeys.."
  ALICE=$(reg_alice getinfo | jq .identity_pubkey -r)
  BOB=$(reg_bob getinfo | jq .identity_pubkey -r)
  CHARLIE=$(reg_charlie getinfo | jq .identity_pubkey -r)
  DAVE=$(reg_dave getinfo | jq .identity_pubkey -r)

  echo "Alice (connected to Gateway, Charlie): $ALICE"
  echo "Bob (connected to Gateway, Dave): $BOB"
  echo "Charlie (connected to Alice, Dave): $CHARLIE"
  echo "Dave (connected to Bob, Charlie): $DAVE"

  echo "Getting addresses.."
  ADDR_ALICE=$(reg_alice newaddress p2wkh | jq .address -r)
  ADDR_BOB=$(reg_bob newaddress p2wkh | jq .address -r)
  ADDR_CHARLIE=$(reg_charlie newaddress p2wkh | jq .address -r)
  ADDR_DAVE=$(reg_dave newaddress p2wkh | jq .address -r)

  ADDR_BTC=$(reg_bitcoin getnewaddress "" legacy)

  echo "generating blocks to $ADDR_BTC"
  reg_bitcoin generatetoaddress 200 $ADDR_BTC > /dev/null
  reg_bitcoin getbalance

  echo "Sending funds.."
  reg_bitcoin sendtoaddress "$ADDR_ALICE" 1
  reg_bitcoin sendtoaddress "$ADDR_BOB" 1
  reg_bitcoin sendtoaddress "$ADDR_CHARLIE" 1
  reg_bitcoin sendtoaddress "$ADDR_DAVE" 1

  mine 6

  # bob -> dave
  reg_bob openchannel --node_key $DAVE --connect regtest-dave-1:9735 --local_amt 15000000 --push_amt 7000000
  mine 6

  # alice -> charlie
  reg_alice openchannel --node_key $CHARLIE --connect regtest-charlie-1:9735 --local_amt 15000000 --push_amt 7000000
  mine 6

  # charlie -> dave
  reg_charlie openchannel --node_key $DAVE --connect regtest-dave-1:9735 --local_amt 15000000 --push_amt 7000000
  mine 6

  # make some connections to sync the graph faster
  reg_alice connect $BOB@regtest-bob-1:9735
  reg_alice connect $DAVE@regtest-dave-1:9735
  reg_charlie connect $BOB@regtest-bob-1:9735
}

function stop() {
  $COMPOSE down --volumes
}

function mine() {
  NUMBLOCKS=6
  if [ ! -z "$1" ]
  then
    NUMBLOCKS=$1
  fi
  reg_bitcoin generatetoaddress $NUMBLOCKS $(reg_bitcoin getnewaddress "" legacy) > /dev/null
}

function reg_alice() {
  docker exec -ti regtest-alice-1 lncli --network regtest "$@"
}

function reg_bob() {
  docker exec -ti regtest-bob-1 lncli --network regtest "$@"
}

function reg_charlie() {
  docker exec -ti regtest-charlie-1 lncli --network regtest "$@"
}

function reg_dave() {
  docker exec -ti regtest-dave-1 lncli --network regtest "$@"
}

function waitnodestart() {
  while ! reg_alice getinfo | grep -q identity_pubkey; do
    sleep 1
  done
  while ! reg_bob getinfo | grep -q identity_pubkey; do
    sleep 1
  done
  while ! reg_charlie getinfo | grep -q identity_pubkey; do
    sleep 1
  done
  while ! reg_dave getinfo | grep -q identity_pubkey; do
    sleep 1
  done
}

function copymacaroons() {
  # save macaroons and TLS cert to folder outside of docker so we can use them from the IDE
  rm -rf $DATA_DIR/{alice,bob,charlie,dave}
  mkdir -p $DATA_DIR/{alice,bob,charlie,dave}

  docker cp regtest-alice-1:/root/.lnd/data/chain/bitcoin/regtest/. $DATA_DIR/alice/
  docker cp regtest-alice-1:/root/.lnd/tls.cert $DATA_DIR/alice/tls.cert

  docker cp regtest-bob-1:/root/.lnd/data/chain/bitcoin/regtest/. $DATA_DIR/bob/
  docker cp regtest-bob-1:/root/.lnd/tls.cert $DATA_DIR/bob/tls.cert

  docker cp regtest-charlie-1:/root/.lnd/data/chain/bitcoin/regtest/. $DATA_DIR/charlie/
  docker cp regtest-charlie-1:/root/.lnd/tls.cert $DATA_DIR/charlie/tls.cert

  docker cp regtest-dave-1:/root/.lnd/data/chain/bitcoin/regtest/. $DATA_DIR/dave/
  docker cp regtest-dave-1:/root/.lnd/tls.cert $DATA_DIR/dave/tls.cert
}

CMD=$1
shift
$CMD "$@"
