# START: begin
CONFIG_PATH=${HOME}/.proglog/

.PHONY: init
init:
	mkdir -p ${CONFIG_PATH}

.PHONY: gencert
gencert:
	cfssl gencert \
		-initca test/ca-csr.json | cfssljson -bare ca

	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=server \
		test/server-csr.json | cfssljson -bare server

	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/ca-config.json \
		-profile=client \
		test/client-csr.json | cfssljson -bare client 

	mv *.pem *.csr ${CONFIG_PATH}

# END: begin

# # START: auth
# $(CONFIG_PATH)/model.conf:
# 	cp test/model.conf $(CONFIG_PATH)/model.conf

# $(CONFIG_PATH)/policy.csv:
# 	cp test/policy.csv $(CONFIG_PATH)/policy.csv

# START: begin
.PHONY: test
# END: auth
test:
# END: begin
# START: auth
#test: $(CONFIG_PATH)/policy.csv $(CONFIG_PATH)/model.conf
#: START: begin
	go test -race ./...
# END: auth

.PHONY: compile
compile:
	protoc api/v1/*.proto \
		--go_out=. \
		--go-grpc_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		--proto_path=.

# END: begin
