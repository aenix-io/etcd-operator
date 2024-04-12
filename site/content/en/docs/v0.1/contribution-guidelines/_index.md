---
title: Contribution Guidelines
weight: 10
description: How to contribute to the project
---

etcd operator guide for developers.

## How to start contributing
First of all, etcd operator uses kubebuilder. [See official docs](https://book.kubebuilder.io/introduction) to learn
more.

### Easy way
Using this way, you don't be able to debug the controller locally. After every change you will have to redeploy changes.

#### Pre-requisites
- Any docker-like container tool, "docker" by default. For more information search for: `CONTAINER_TOOL` in Makefile.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

**Steps**
1. Create and prepare kind cluster:
    ```shell
    make kind-prepare
    ```

2. Build image and load it into kind cluster:
    ```shell
    make kind-load
    ```

3. Deploy CRDs, etcd-operator, RBAC, webhook certs into kind cluster:
    ```shell
    make deploy
    ```

4. To deploy your code changes, load a new image and redeploy etcd-operator:
    ```shell
    make kind-load && make redeploy
    ```

5. To clean up after all, delete kind cluster:
    ```shell
    make kind-delete
    ```
### Advanced way
Using VSCode you can connect your IDE to a pod in kubernetes cluster and use `go run cmd/main.go` from the pod.
It will allow you to debug easily and faster integrate code changes to you develop environment.

**General steps**
1. Install VSCode Kubernetes extension.
2. Install VSCode Dev Containers extension.
3. Create pvc to store operator code, go modules and your files.
4. Build Dockerfile to create image for your develop environment (git, golang , etc...). Take base image that you love
most.
5. Patch operator deployment to
   * Attach PVC from step 3.
   * Change operator image to your image.
   * Change command and args to endless sleep loop.
   * Change rolling update strategy to recreate if you use RWO storage class.
   * Change security context RunAsNonRoot to false.
   * Increase requests and limits.
   * Remove readiness and liveness probes.
6. Attach VSCode to a running container through Kubernetes extension.
7. Install necessary extensions to the container. They will be preserved after container restart if you attached pvc to
home directory.
8. Run `go run cmd/main.go`.


## Release procedure
Every PR should have a label to show what kind of changes does it bring. For example, PRs with docs changes should have
`documentation` label. This labels will be used by the `release-drafter` workflow to prepare the release draft.

### Cutting off a release
When all tasks for the release are merged to `main` branch, maintainer should

- create new minor or major version in `site` if it's not a patch release and merge this PR to `main` branch
- check that tag has not been already created. If it has been, delete it
- publish the release using draft, created by `release-drafter`
- you are amazing :)
