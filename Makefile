os = $(shell uname | tr '[A-Z]' '[a-z]')
arch = $(shell uname -m | sed 's/x86_64/amd64/')
tendermint_version = 0.25.0
tendermint_binary = tendermint_${tendermint_version}_${os}_${arch}
tendermint_archive = ${tendermint_binary}.zip

tendermint-cas-demo:
	@go build ./cmd/tendermint-cas-demo

${tendermint_binary}: ${tendermint_archive}
	@unzip -o $<
	@mv tendermint $@
	@touch $@

${tendermint_archive}:
	@echo downloading ${tendermint_archive}...
	@curl -o $@ -Ss -L https://github.com/tendermint/tendermint/releases/download/v${tendermint_version}/${tendermint_archive}
	@touch $@

.PHONY: bootstrap_1
bootstrap_1: ${tendermint_binary} tendermint-cas-demo
	@./bootstrap_1.sh ./${tendermint_binary}

.PHONY: bootstrap_3
bootstrap_3: ${tendermint_binary} tendermint-cas-demo
	@./bootstrap_3.sh ./${tendermint_binary}

.PHONY: clean
clean:
	@rm -rf tendermint-cas-demo
	@rm -rf ${tendermint_binary} ${tendermint_archive}
	@rm -rf tendermint_zero/ tendermint_{a,b,c}/
	@rm -rf zero.json {a,b,c}.json
