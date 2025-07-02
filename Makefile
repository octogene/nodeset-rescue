VERSION = v1.0.0
BUILD_TAGS ?= ns
IMAGE_NAME ?= nsrescuenode/rescue-proxy
BUILD_TAGS_ARG = $(if $(BUILD_TAGS),-tags=$(BUILD_TAGS))

ifeq ($(BUILD_TAGS), ns)
	export GOEXPERIMENT=synctest
endif

SOURCEDIR := .
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')
PROTO_IN := proto
PROTO_OUT := pb
PROTO_DEPS := $(wildcard $(PROTO_IN)/*.proto)

.PHONY: all
all: protos
	go build $(BUILD_TAGS_ARG) .

.PHONY: protos
protos: $(PROTO_DEPS)
	protoc -I=./$(PROTO_IN) --go_out=paths=source_relative:$(PROTO_OUT) \
		--go-grpc_out=paths=source_relative:$(PROTO_OUT) $(PROTO_DEPS)

SW_DIR := executionlayer/stakewise
ABI_DIR := $(SW_DIR)/abis
$(SW_DIR)/vaults-registry-encoding.go: $(ABI_DIR)/vaults-registry.json
	go run github.com/ethereum/go-ethereum/cmd/abigen@v1.15.11 --v2 --abi $< --pkg stakewise --type vaultsRegistry --out $@
$(SW_DIR)/eth-priv-vault-encoding.go: $(ABI_DIR)/eth-priv-vault.json
	go run github.com/ethereum/go-ethereum/cmd/abigen@v1.15.11 --v2 --abi $< --pkg stakewise --type ethPrivVault --out $@

.PHONY: clean
clean:
	rm -f pb/*
	rm -f api-client

.PHONY: docker
docker: all
	docker build $(BUILD_TAGS_ARG) . -t $(IMAGE_NAME):$(VERSION)
	docker tag $(IMAGE_NAME):$(VERSION) $(IMAGE_NAME):latest
	docker tag $(IMAGE_NAME):$(VERSION) rescue-proxy:latest

.PHONY: publish
publish:
	docker push $(IMAGE_NAME):latest
	docker push $(IMAGE_NAME):$(VERSION)

.DELETE_ON_ERROR: cov.out
cov.out: $(SOURCES)
	go test -coverprofile=cov.out $(BUILD_TAGS_ARG) ./...

.PHONY: testcov
testcov: cov.out
	go tool cover -html=cov.out

./api-client: protos
	go build -o api-client api/client/main.go
