#!/usr/bin/env fish

function prereqs
    echo
    echo "    Download Tendermint v0.25.0 for your system from"
    echo "    https://github.com/tendermint/tendermint/releases/tag/v0.25.0"
    echo "    and place the tendermint binary in your PATH."
    echo
end

if test (type tendermint >/dev/null 2>&1)
    echo tendermint binary is required
    prereqs
    exit
end

if test ! (string match -r 0.25.0 (tendermint version))
    echo tendermint v0.25.0 is required
    prereqs
    exit
end

echo removing any old state
rm -rf tendermint_{a,b,c}
rm -rf {a,b,c}.json

echo initializing 3 nodes
tendermint init --home tendermint_a >/dev/null 2>&1
tendermint init --home tendermint_b >/dev/null 2>&1
tendermint init --home tendermint_c >/dev/null 2>&1

echo producing common genesis file
set a (cat tendermint_a/config/genesis.json | jq .validators[0])
set b (cat tendermint_b/config/genesis.json | jq .validators[0])
set c (cat tendermint_c/config/genesis.json | jq .validators[0])
set common_genesis (cat tendermint_a/config/genesis.json | jq "(.validators) = [$a, $b, $c]")
echo $common_genesis | jq . > tendermint_a/config/genesis.json
echo $common_genesis | jq . > tendermint_b/config/genesis.json
echo $common_genesis | jq . > tendermint_c/config/genesis.json

echo producing config files
set a (cat tendermint_a/config/genesis.json | jq .validators[0].address | tr -d '"')
set b (cat tendermint_b/config/genesis.json | jq .validators[0].address | tr -d '"')
set c (cat tendermint_c/config/genesis.json | jq .validators[0].address | tr -d '"')
set persistent_peers "$a@127.0.0.1:10001, $b@127.0.0.1:10002, $c@127.0.0.1:10003"
echo 'moniker = "a"'                            | tee    tendermint_a/config/config.toml >/dev/null
echo 'moniker = "b"'                            | tee    tendermint_b/config/config.toml >/dev/null
echo 'moniker = "c"'                            | tee    tendermint_c/config/config.toml >/dev/null
echo 'proxy_app = ""'                           | tee -a tendermint_?/config/config.toml >/dev/null
echo ''                                         | tee -a tendermint_?/config/config.toml >/dev/null
echo '[rpc]'                                    | tee -a tendermint_?/config/config.toml >/dev/null
echo 'laddr = ""'                               | tee -a tendermint_?/config/config.toml >/dev/null
echo ''                                         | tee -a tendermint_?/config/config.toml >/dev/null
echo '[p2p]'                                    | tee -a tendermint_?/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10001"'          | tee -a tendermint_a/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10002"'          | tee -a tendermint_b/config/config.toml >/dev/null
echo 'laddr = "tcp://127.0.0.1:10003"'          | tee -a tendermint_c/config/config.toml >/dev/null
echo "persistent_peers = \"$persistent_peers\"" | tee -a tendermint_?/config/config.toml >/dev/null

echo now you can run three nodes
echo
echo "    td -api-addr 127.0.0.1:8081 -app-file a.json -tendermint-dir tendermint_a"
echo "    td -api-addr 127.0.0.1:8082 -app-file b.json -tendermint-dir tendermint_b"
echo "    td -api-addr 127.0.0.1:8083 -app-file c.json -tendermint-dir tendermint_c"
echo
echo other fun things to try
echo 
echo "    watch -n1 -- cat ?.json                            # watch state being updated"
echo "    curl -Ss -XPOST 'localhost:8081/x?new=one'         # set x=one on node a"
echo "    curl -Ss -XPOST 'localhost:8082/x?old=one&new=two' # set x=two on node b"
echo "    curl -Ss -XGET  'localhost:8083/x'                 # get x on node c"
echo

