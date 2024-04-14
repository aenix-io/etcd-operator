# Design

This document describes the interaction between `EtcdCluster` custom resources and other Kubernetes primitives and gives an overview of the underlying implementation.

## Creating a cluster

When a user adds an `EtcdCluster` resource to the Kubernetes cluster, the operator responds by creating
* A configmap holding configuration values for bootstrapping a new cluster (`ETCD_INITIAL_CLUSTER_*` environment variables).
* A headless service for intra-cluster communication.
* A statefulset with pods for the individual members of the etcd cluster.
* A service for clients' access to the etcd cluster.
* A pod disruption budget to prevent the etcd cluster from losing quorum.

If the above is successful, the etcd cluster status is set to `Initialized`.
