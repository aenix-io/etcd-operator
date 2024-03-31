---
title: Quickstart
weight: 0
description: Get etcd with etcd-operator up and running in less than 5 minutes!
---

Follow these instructions to install, run, and test etcd with etcd-operator in a Kubernetes cluster.

Pre-requisites:
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
- [kustomize](https://github.com/kubernetes-sigs/kustomize)
- Kubernetes cluster and `kubectl` configured to use it
  - If you don't have a Kubernetes cluster, you can use [kind](https://kind.sigs.k8s.io/docs/user/quick-start/) to create a local one
- [cert-manager](https://cert-manager.io/docs/installation/) installed in the cluster

1. Install etcd-operator:
    ```bash
    kustomize build 'https://github.com/aenix-io/etcd-operator//config/default?ref=main' | kubectl apply -f -
    ```
2. Check the operator is running:
    ```bash
    kubectl get pods -n etcd-operator-system -l control-plane=controller-manager
    ```
3. Create an etcd cluster:
    ```bash
    kubectl apply -f https://github.com/aenix-io/etcd-operator/raw/main/config/samples/etcd.aenix.io_v1alpha1_etcdcluster.yaml
    ```
   **Caution**: by default emptyDir storage is used. It means such cluster configuration is not intended for long-term storage.

4. Check the etcd cluster is running:
    ```bash
    kubectl get pods -l app.kubernetes.io/managed-by=etcd-operator
    ```
