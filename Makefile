# Tencent is pleased to support the open source community by making TKEStack available.
#
# Copyright (C) 2012-2019 Tencent. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use
# this file except in compliance with the License. You may obtain a copy of the
# License at
#
# https://opensource.org/licenses/Apache-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OF ANY KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations under the License.
#

REGISTRY_NAME?=docker.io/tkestack

IMAGE_TAGS?=v1.0.3

# A "canary" image gets built if the current commit is the head of the remote "master" branch.
# That branch does not exist when building some other branch in TravisCI.
IMAGE_TAGS+=$(shell if [ "$$(git rev-list -n1 HEAD)" = "$$(git rev-list -n1 origin/master 2>/dev/null)" ]; then echo "canary"; fi)

# A "X.Y.Z-canary" image gets built if the current commit is the head of a "origin/release-X.Y.Z" branch.
# The actual suffix does not matter, only the "release-" prefix is checked.
IMAGE_TAGS+=$(shell git branch -r --points-at=HEAD | grep 'origin/release-' | grep -v -e ' -> ' | sed -e 's;.*/release-\(.*\);\1-canary;')

# A release image "vX.Y.Z" gets built if there is a tag of that format for the current commit.
# --abbrev=0 suppresses long format, only showing the closest tag.
IMAGE_TAGS+=$(shell tagged="$$(git describe --tags --match='v*' --abbrev=0)"; if [ "$$tagged" ] && [ "$$(git rev-list -n1 HEAD)" = "$$(git rev-list -n1 $$tagged)" ]; then echo $$tagged; fi)

# Images are named after the command contained in them.
ifeq ($(GOARCH),arm64)
	IMAGE_NAME=$(REGISTRY_NAME)/csi-operator-$(GOARCH)
else
	IMAGE_NAME=$(REGISTRY_NAME)/csi-operator
endif

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

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	$(shell if [ ! -f "controller-gen" ];then go install sigs.k8s.io/controller-tools/cmd/controller-gen;fi)
	controller-gen crd paths=./pkg/apis/... output:crd:dir=./config/crds

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

# Build and push the docker image
image: csi-operator
	set -ex; \
	cp output/bin/csi-operator build/docker; \
	docker build -t ${IMAGE_NAME}:latest build/docker; \
	rm build/docker/csi-operator; \
	for tag in $(IMAGE_TAGS); do \
	  docker tag ${IMAGE_NAME}:latest ${IMAGE_NAME}:$$tag; \
	  docker push ${IMAGE_NAME}:$$tag; \
	done
