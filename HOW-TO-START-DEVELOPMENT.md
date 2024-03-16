
# How to start contributing
## Build your develop environment
### Easy way
1. Download and install [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#creating-a-cluster).
2. Create lind cluster `kind create cluster --name etcd-operator-kind`.
3. Switch kubectl context to kind `kubectl cluster-info --context kind-etcd-operator-kind`. Be attentive to avoid damaging your production environment.
4. Install cert-manager (it creates certificate k8s secrets). Refer to the [docs](https://cert-manager.io/docs/installation/helm/#4-install-cert-manager).
5. Build docker image *controller:latest* `make docker-build`.
6. Retag image to upload to kind cluster with correct name `docker tag controller:latest ghcr.io/aenix-io/etcd-operator:latest`.
7. Load image to kind cluster `kind load docker-image ghcr.io/aenix-io/etcd-operator:latest`.
8. Install CRDs `make install`.
9. Deploy operator, RBAC, webhook certs `make deploy`.

To deploy your code changes
1. Rebuild the image `make docker-build`.
2. Retag image to upload to kind cluster with correct name `docker tag controller:latest ghcr.io/aenix-io/etcd-operator:latest`.
3. Load image to kind cluster `kind load docker-image ghcr.io/aenix-io/etcd-operator:latest`.
4. Redeploy yaml manifests if necessary `make deploy`.
5. Restart etcd-operator `kubectl rollout restart -n etcd-operator-system deploy/etcd-operator-controller-manager`.

### Advanced way
Using *easy way* you will not be able to debug golang code locally and every change will be necessary to build as an image, upload to the cluster and then restart operator.
If you use VSCode, you can connect your IDE to a pod in kubernetes cluster and use `go run cmd/main.go` from the pod. It will allow you to debug easily and faster integrate code changes to you develop environment.

General steps.
1. Install VSCode Kubernetes extension.
2. Install VSCode Dev Containers extension.
3. Create pvc to store operator code, go modules and your files.
4. Build Dockerfile to create image for your develop environment (git, golang , etc...). Take base image that you love most.
5. Patch operator deployment to
  * Attach PVC from step 3.
  * Change operator image to your image.
  * Change command and args to endless sleep loop.
  * Change rolling update strategy to recreate if you use RWO storage class.
  * Change security context RunAsNonRoot to false.
  * Increase requests and limits.
  * Remove readiness and liveness probes.
6. Attach VSCode to a running container through Kubernetes extension.
7. Install necessary extensions to the container. They will be preserved after container restart if you attached pvc to home directory.
8. Run `go run cmd/main.go`.

## Read kubebuilder documentation
[Docs](https://book.kubebuilder.io/introduction)
