
# Image URL to use all building/pushing image targets
IMG ?= csi-operator:latest

all: test csi-operator

# Run tests
test: generate fmt vet revive manifests
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build csi-operator binary
csi-operator: generate fmt vet revive
	go build -o output/bin/csi-operator tkestack.io/csi-operator/cmd/csi-operator

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet revive
	go run ./cmd/csi-operator/main.go

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crds

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	kubectl apply -f config/crds
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	$(shell if [ ! -f "controller-gen" ];then go install sigs.k8s.io/controller-tools/cmd/controller-gen;fi)
	controller-gen all

# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Run go vet against code
vet:
	go vet ./pkg/... ./cmd/...

# Run revive against code
revive:
	files=$$(find . -name '*.go' | egrep -v './vendor|zz_generated'); \
	revive -config build/linter/revive.toml -formatter friendly $$files

# Generate code
generate:
ifndef GOPATH
	$(error GOPATH not defined, please define GOPATH. Run "go help gopath" to learn more about GOPATH)
endif
	$(shell if [ ! -f "deepcopy-gen" ];then go install k8s.io/code-generator/cmd/deepcopy-gen;fi)
	go generate ./pkg/... ./cmd/...

# Build the docker image
docker-build: test
	cp output/bin/csi-operator build/docker
	docker build -t ${IMG} build/docker
	rm build/docker/csi-operator

# Push the docker image
docker-push:
	docker push ${IMG}
