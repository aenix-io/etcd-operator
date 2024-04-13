---
title: Quickstart
weight: 0
description: Get etcd with etcd-operator up and running in less than 5 minutes!
---

Follow these instructions to install, run, and test etcd with etcd-operator in a Kubernetes cluster.

Pre-requisites:
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- Kubernetes cluster and `kubectl` configured to use it
- If you don't have a Kubernetes cluster, you can use [kind](https://kind.sigs.k8s.io/docs/user/quick-start/) to create a local one
- [cert-manager](https://cert-manager.io/docs/installation/) installed in the cluster

1. Install etcd-operator:
    ```bash
    kubectl apply -f https://github.com/aenix-io/etcd-operator/releases/download/latest/etcd-operator.yaml
    ```
2. Check the operator is running:
    ```bash
    kubectl get pods -n etcd-operator-system -l control-plane=controller-manager
    ```
3. Create a simple etcd cluster:
    ```bash
    kubectl apply -f https://github.com/aenix-io/etcd-operator/raw/main/examples/manifests/etcdcluster-simple.yaml
    ```
   **Caution**: by default emptyDir storage is used. It means such cluster configuration is not intended for long-term storage.

4. Check the etcd cluster is running:
    ```bash
    kubectl get pods -l app.kubernetes.io/managed-by=etcd-operator
    ```
