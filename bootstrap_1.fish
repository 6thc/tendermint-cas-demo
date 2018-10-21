#!/usr/bin/env fish

function prereqs
    echo
    echo "    Download Tendermint v0.25.0 for your system from"
    echo "    https://github.com/tendermint/tendermint/releases/tag/v0.25.0"
    echo "    and place the tendermint binary in your PATH."
    echo
end

if test (type tendermint >/dev/null 2>&1)
    echo tendermint binary is required in PATH
    prereqs
    exit
end

if test ! (string match -r 0.25.0 (tendermint version))
    echo tendermint v0.25.0 is required
    prereqs
    exit
end

echo removing any old state
rm -rf tendermint_zero
rm -rf zero.json

echo initializing node
tendermint init --home tendermint_zero >/dev/null 2>&1

echo producing config files
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
echo "    td -api-addr 127.0.0.1:8081 -app-file zero.json -tendermint-dir tendermint_zero"
echo
echo other fun things to try
echo 
echo "    watch -n1 -- cat zero.json                         # watch state being updated"
echo "    curl -Ss -XPOST 'localhost:8081/x?new=one'         # set x=one"
echo "    curl -Ss -XPOST 'localhost:8081/x?old=one&new=two' # set x=two"
echo "    curl -Ss -XGET  'localhost:8081/x'                 # get x"
echo
