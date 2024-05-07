# Design

This document describes the interaction between `EtcdCluster` custom resources and other Kubernetes
primitives and gives an overview of the underlying implementation.

## Reconciliation flowchart

```mermaid
flowchart TD
    Start(Start) --> A0[Ensure\nservice]
      A0 --> A1[Connect to the cluster\nand fetch all statuses]
        A1 --> |Got some response| AA{Is cluster\nin quorum?}
          AA -->|Yes| AAA{All reachable\nmembers have the\nsame cluster ID?}
            AAA -->|Yes| AAAA[Promote any learners.]
              AAAA -->|OK| AAAA0[Ensure configmap with initial cluster\nmatching existing members and\ncluster state=existing]
              AAAA0 -->|OK| AAAA1[Ensure StatefulSet with\nreplicas = max member ordinal + 1]
              AAAA1 -->|OK| AAAAA{Have all members\nbeen reached?}
                AAAAA -->|Yes| AAAAAA{Is it\nready?}
                  AAAAAA -->|Yes| AAAAAAA{Is its size\nequal to the\nnumber of\n members?}
                    AAAAAAA -->|Yes| AAAAAAAA{Is the\nEtcdCluster\nsize equal to the\nStatefulSet\nsize?}
                      AAAAAAAA -->|Yes| AAAAAAAAA[Set cluster\nstatus to ready.]
                        AAAAAAAAA --> HappyStop([Stop])

                      AAAAAAAA --> |No, desired\nsize larger| AAAAAAAAB[Ensure ConfigMap with\ninitial cluster state existing\nand initial cluster URLs\nequal to current cluster\nplus one member, do\n'member add' API call and\nincrement StatefulSet size.]
                        AAAAAAAAB --> ScaleUpStop([Stop])

                      AAAAAAAA --> |No, desired\nsize smaller| AAAAAAAAC[Member remove API\ncall, then decrement\nStatefulSet size\nthen delete PVC.]
                        AAAAAAAAC --> ScaleDownStop([Stop])

                      AAAAAAAA --> |Etcd replicas=0\nSTS replicas=1| AAAAAAAAD[Decrement\nSTS to zero]
                        AAAAAAAAD --> ScaleToZeroStop([Stop])

                    AAAAAAA -->|No,\ngreater| AAAAAAAB([This is 146%\nsplitbrain, stop.])

                    AAAAAAA -->|No,\nsmaller| AAAAAAAC([StatefulSetController\nis not working as\nit should, stop.])

                  AAAAAA -->|No| AAAAAAB[The non-ready replicas\nare evicted members,\nthey should be removed.]
  
                AAAAA -->|No| AAAAAB{asd}

              AAAA -->|Error| AAAAB([Requeue])
              AAAA0 -->|Error| AAAAB([Requeue])
              AAAA1 -->|Error| AAAAB([Requeue])

            AAA -->|No| AAAB[Cluster is in\nsplit-brain. Set\nerror status.]
              AAAB --> AAABStop([Stop])

        A1 --> |No members\nreached| AB{EtcdCluster\n.spec.replicas==0?}
        A1 --> |Unexpected\nerror| AC(Requeue)
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
