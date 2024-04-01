# How to start contributing

## Read kubebuilder documentation
[Docs](https://book.kubebuilder.io/introduction)

## Build your develop environment

### Easy way
1. Create and prepare kind cluster:
    ```shell
    make kind-prepare
    ```
2. Install CRDs into kind cluster
    ```shell
    make install
    ```

3. Build image and load it into kind cluster, deploy etcd-operator, RBAC, webhook certs
    ```shell
    make deploy
    ```

4. To deploy your code changes, redeploy etcd-operator:
    ```shell
    make redeploy
    ```

5. To clean up after all, delete kind cluster:
    ```shell
    make kind-delete
    ```

### Advanced way
Using *easy way* you will not be able to debug golang code locally and every change will be necessary to build as an image, upload to the cluster and then restart operator.
If you use VSCode, you can connect your IDE to a pod in kubernetes cluster and use `go run cmd/main.go` from the pod. It will allow you to debug easily and faster integrate code changes to you develop environment.

**General steps**
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
