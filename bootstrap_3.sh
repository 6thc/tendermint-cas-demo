#!/usr/bin/env sh

set -e

if [ $# -ne 1 ]
then
    echo "usage: $0 <tendermint binary>"
    exit
fi

tendermint_binary=$1

echo clearing any old state...
rm -rf tendermint_{a,b,c}/
rm -rf {a,b,c}.json

echo initializing three nodes...
${tendermint_binary} init --home tendermint_a >/dev/null
${tendermint_binary} init --home tendermint_b >/dev/null
${tendermint_binary} init --home tendermint_c >/dev/null

echo capturing validators...
a_validator=$(cat tendermint_a/config/genesis.json | jq .validators[0])
b_validator=$(cat tendermint_b/config/genesis.json | jq .validators[0])
c_validator=$(cat tendermint_c/config/genesis.json | jq .validators[0])

echo capturing peer addresses...
a_address=$(echo $a_validator | jq .address | tr -d '"')
b_address=$(echo $b_validator | jq .address | tr -d '"')
c_address=$(echo $c_validator | jq .address | tr -d '"')
persistent_peers="${a_address}@127.0.0.1:10001, ${b_address}@127.0.0.1:10002, ${c_address}@127.0.0.1:10003"

echo building a common genesis file...
common_genesis=$(cat tendermint_a/config/genesis.json | jq "(.validators) = [${a_validator}, ${b_validator}, ${c_validator}]")

echo writing common genesis file...
echo $common_genesis | jq . > tendermint_a/config/genesis.json
echo $common_genesis | jq . > tendermint_b/config/genesis.json
echo $common_genesis | jq . > tendermint_c/config/genesis.json

echo producing config files...
echo 'moniker = "a"'                              | tee    tendermint_a/config/config.toml >/dev/null
echo 'moniker = "b"'                              | tee    tendermint_b/config/config.toml >/dev/null
echo 'moniker = "c"'                              | tee    tendermint_c/config/config.toml >/dev/null
echo 'proxy_app = ""'                             | tee -a tendermint_?/config/config.toml >/dev/null
echo ''                                           | tee -a tendermint_?/config/config.toml >/dev/null
echo '[rpc]'                                      | tee -a tendermint_?/config/config.toml >/dev/null
echo 'laddr = ""'                                 | tee -a tendermint_?/config/config.toml >/dev/null
echo ''                                           | tee -a tendermint_?/config/config.toml >/dev/null
echo '[p2p]'                                      | tee -a tendermint_?/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10001"'            | tee -a tendermint_a/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10002"'            | tee -a tendermint_b/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10003"'            | tee -a tendermint_c/config/config.toml >/dev/null
echo "persistent_peers = \"${persistent_peers}\"" | tee -a tendermint_?/config/config.toml >/dev/null

echo now you can run three nodes
echo
echo "    ./tendermint-cas-demo -api-addr 127.0.0.1:8081 -app-file a.json -tendermint-dir tendermint_a"
echo "    ./tendermint-cas-demo -api-addr 127.0.0.1:8082 -app-file b.json -tendermint-dir tendermint_b"
echo "    ./tendermint-cas-demo -api-addr 127.0.0.1:8083 -app-file c.json -tendermint-dir tendermint_c"
echo
echo other fun things to try
echo
echo "    watch -n1 -- cat ?.json                            # watch state being updated"
echo "    curl -Ss -XPOST 'localhost:8081/x?new=one'         # set x=one"
echo "    curl -Ss -XPOST 'localhost:8082/x?old=one&new=two' # set x=two"
echo "    curl -Ss -XGET  'localhost:8083/x'                 # get x"
echo
