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
              AAAAA2 --> |OK| AAAAAA{Are all\nmembers healthy?}
                AAAAAA --> |Yes| AAAAAAA{Are all STS pods present\nin the member list?}
                  AAAAAAA --> |Yes| AAAAAAAA{Is the\nEtcdCluster\nsize equal to the\nStatefulSet\nsize?}
                    AAAAAAAA -->|Yes| AAAAAAAAA[Set cluster\nstatus to ready.]
                      AAAAAAAAA --> HappyStop([Stop])

                    AAAAAAAA --> |No, desired\nsize larger| AAAAAAAAB[Ensure ConfigMap with\ninitial cluster state existing\nand initial cluster URLs\nequal to current cluster\nplus one member, do\n'member add' API call and\nincrement StatefulSet size.]
                      AAAAAAAAB --> ScaleUpStop([Stop])

                    AAAAAAAA --> |No, desired\nsize smaller| AAAAAAAAC[Member remove API\ncall, then decrement\nStatefulSet size\nthen delete PVC.]
                      AAAAAAAAC --> ScaleDownStop([Stop])

                    AAAAAAAA --> |Etcd replicas=0\nSTS replicas=1| AAAAAAAAD[Decrement\nSTS to zero]
                      AAAAAAAAD --> ScaleToZeroStop([Stop])

                AAAAAA --> |No| AAAAAAB1[On timeout evict member.]
                  AAAAAAB1 --> AAAAAAB2[Delete PVC, ensure ConfigMap with\nmembers + this one and delete pod.]

                AAAAAAA --> |No| AAAAAAB2

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
