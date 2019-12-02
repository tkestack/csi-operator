# csi-operator

Operator that deploys and updates CSI driver and external components in Kubernetes cluster.

## Build

Please install `revive` before building:

```
go get -u github.com/mgechev/revive
```

To make the binary, just run:

```bash
make csi-operator
```

The binary will be located at `output/bin/csi-operator`.

To release the image, just run:

```bash
make docker-build
```

Then a image with name `csi-operator:latest` will be created.

## Usage

`csi-operator` can be deployed inside the kubernetes cluster:

1. Create the RBAC objects needed by `csi-operator`:
    ```bash
    kubectl -f deploy/kubernetes/rbac.yaml
    ```

2. Create a deployment to run the `csi-operator`:
    ```bash
    kubectl -f deploy/kubernetes/deployment.yaml
    ```

## Examples

There are a large number of examples in [examples](examples/).
