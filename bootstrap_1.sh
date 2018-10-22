#!/usr/bin/env sh

set -e

if [ $# -ne 1 ]
then
    echo "usage: $0 <tendermint binary>"
    exit
fi

tendermint_binary=$1

echo clearing any old state...
rm -rf tendermint_zero/
rm -rf zero.json

echo initializing one node...
${tendermint_binary} init --home tendermint_zero >/dev/null

echo producing config file...
echo 'moniker = "zero"'                 > tendermint_zero/config/config.toml
echo 'proxy_app = ""'                  >> tendermint_zero/config/config.toml
echo ''                                >> tendermint_zero/config/config.toml
echo '[rpc]'                           >> tendermint_zero/config/config.toml
echo 'laddr = ""'                      >> tendermint_zero/config/config.toml
echo ''                                >> tendermint_zero/config/config.toml
echo '[p2p]'                           >> tendermint_zero/config/config.toml
echo 'laddr = "tcp://127.0.0.1:10000"' >> tendermint_zero/config/config.toml

echo now you can run one node
echo
echo "    ./tendermint-cas-demo -api-addr 127.0.0.1:8081 -app-file zero.json -tendermint-dir tendermint_zero"
echo
echo other fun things to try
echo
echo "    watch -n1 -- cat zero.json                         # watch state being updated"
echo "    curl -Ss -XPOST 'localhost:8081/x?new=one'         # set x=one"
echo "    curl -Ss -XPOST 'localhost:8081/x?old=one&new=two' # set x=two"
echo "    curl -Ss -XGET  'localhost:8081/x'                 # get x"
echo
