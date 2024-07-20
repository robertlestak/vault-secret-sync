
.PHONY: codegen
codegen:
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
	controller-gen crd:crdVersions=v1 paths="./..." output:crd:dir="./deploy/charts/vault-secret-sync/charts/vault-secret-sync-operator/crds"

.PHONY: docker-dev
docker-dev:
	docker build --platform linux/amd64 -t vault-secret-sync:latest .

devcerts:
	mkdir -p $@
	openssl genrsa -out $@/ca.key 2048
	openssl req -x509 -new -nodes -key $@/ca.key -subj "/CN=Test CA" -days 3650 -out $@/ca.crt
	openssl genrsa -out $@/server.key 2048
	openssl req -new -key $@/server.key -subj "/CN=localhost" -out $@/server.csr
	openssl x509 -req -in $@/server.csr -CA $@/ca.crt -CAkey $@/ca.key -CAcreateserial -out $@/server.crt -days 3650
	openssl genrsa -out $@/client.key 2048
	openssl req -new -key $@/client.key -subj "/CN=Test Client" -out $@/client.csr
	openssl x509 -req -in $@/client.csr -CA $@/ca.crt -CAkey $@/ca.key -CAcreateserial -out $@/client.crt -days 3650


.PHONY: test
test:
	@echo "Running tests..."
	@go test -v ./...
	@govulncheck -show verbose ./...