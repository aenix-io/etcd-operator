# Design

This document describes the interaction between `EtcdCluster` custom resources and other Kubernetes
primitives and gives an overview of the underlying implementation.

## Reconciliation flowchart

```mermaid
flowchart TD
    Start(Start) --> A0[Ensure service.]
      A0 --> A1[Connect to the cluster\nand fetch all statuses.]
      A1 --> |Got some response| A2[Ensure ConfigMap has\nETCD_FORCE_NEW_CLUSTER=false.]
      A2 --> AA{All reachable\nmembers have the\nsame cluster ID?}
        AA --> |Yes| AAA{Is cluster\nin quorum?}
          AAA --> |Yes| AAAA{Are all members \nmanaged by the operator?}
            AAAA --> |Yes| AAAAA0[Promote any learners.]
              AAAAA0 --> |OK| AAAAA1[Ensure configmap with initial cluster\nmatching existing members and\ncluster state=existing]
              AAAAA1 --> |OK| AAAAA2[Ensure StatefulSet with\nreplicas = max member ordinal + 1]
              AAAAA2 --> |OK| AAAAA3{Are all\nmembers healthy?}
              AAAAA3 --> |Yes| AAAAAA{Are all STS pods present\nin the member list?}
                AAAAAA --> |Yes| AAAAAAA{Is the\nEtcdCluster\nsize equal to the\nStatefulSet\nsize?}
                  AAAAAAA -->|Yes| AAAAAAAA[Set cluster\nstatus to ready.]
                    AAAAAAAA --> HappyStop([Stop])

                  AAAAAAA --> |No, desired\nsize larger| AAAAAAAB[Ensure ConfigMap with\ninitial cluster state existing\nand initial cluster URLs\nequal to current cluster\nplus one member, do\n'member add' API call and\nincrement StatefulSet size.]
                    AAAAAAAB --> ScaleUpStop([Stop])

                  AAAAAAA --> |No, desired\nsize smaller| AAAAAAAC[Member remove API\ncall, then decrement\nStatefulSet size\nthen delete PVC.]
                    AAAAAAAC --> ScaleDownStop([Stop])

                  AAAAAAA --> |Etcd replicas=0\nSTS replicas=1| AAAAAAAD[Decrement\nSTS to zero]
                    AAAAAAAD --> ScaleToZeroStop([Stop])

              AAAAA0 -->|Error| AAAAAB([Requeue])
              AAAAA1 -->|Error| AAAAAB([Requeue])
              AAAAA2 -->|Error| AAAAAB([Requeue])

            AAAA --> |No| AAAAB([Not implemented,\nstop.])

          AAA --> |No| AAAB([Either the cluster will\nsoon recover when\nall pods are back online\nor something caused\ndata loss and majority\n failure simultaneously.])

        AA --> |No| AAB[Cluster is in\nsplit-brain. Set\nerror status.]
          AAB --> AABStop([Stop])

      A1 --> |No members\nreached| AB{Is the correct\nzero-replica STS\npresent?}
        AB --> |Yes| ABA{EtcdCluster\n.spec.replicas==0?}
          ABA --> |Yes| ABAA([Cluster successfully\nscaled to zero, stop.])
          ABA --> |No| ABAB[Ensure ConfigMap with\nforce-new-cluster,\ninitial cluster = new,\ninitial cluster peers with\nsingle member `name`-0]
            ABAB --> |OK| ABABA[Increment STS size.]
              ABABA --> |OK| ABABAA([Stop])
              ABABA --> |Error| ABABAB([Requeue])

            ABAB --> |Error| ABABAB

        AB --> |No| ABB{Is the STS\npresent at all?}
          ABB --> |Yes| ABBA[Patch the STS,\nexcept for replicas]
            ABBA --> |OK| ABBAA([Stop])
            ABBA --> |Error| ABBAB([Requeue])

          ABB --> |No| ABBB[Create a zero-\nreplica STS]
            ABBB --> |OK| ABBBA([Stop])
            ABBB --> |Error| ABBBB([Requeue])

      A0 --> |Unexpected\nerror| AC(Requeue)
      A1 --> |Unexpected\nerror| AC(Requeue)
      A2 --> |Unexpected\nerror| A2Err(Requeue)
```
<!---
TODO: Commented this out in favor of flowchart, but some things might come back later
## Creating a cluster

When a user adds an `EtcdCluster` resource to the Kubernetes cluster, the reconciler observes an
`EtcdCluster` object with an empty list of conditions in its status. This prompts it to fill the
status field with a set of default conditions, including an "etcd not ready" condtion with the
reason "waiting for first quorum".

TODO: we need a diagram of possible state transitions for the various conditions. We also need to
better handle the possibility of a bad status being passed when creating a cluster. We should write
tests, where an etcd cluster with a non-empty status field is applied to the cluster. We should also
try to find a way to determine that the cluster is not ready and/or waiting for first quorum without
assuming that a new cluster has an empty status field.

Next, the operator creates the following objects:

* A configmap holding configuration values for bootstrapping a new cluster (`ETCD_INITIAL_CLUSTER_*` environment variables).
* A headless service for intra-cluster communication.
* A statefulset with pods for the individual members of the etcd cluster.
* A service for clients' access to the etcd cluster.
* A pod disruption budget to prevent the etcd cluster from losing quorum.

If the above is successful, the etcd cluster status is set to `Initialized`.

If no error happens, the statefulset is most likely not yet ready and the status is updated with "etcd cluster not ready" as it is "waiting for first quorum". Once the statefulset is ready, a reconciliation is triggered again, since the child statefulset is also being watched. Finally, the status is updated once again to a "ready" condition.
--->
