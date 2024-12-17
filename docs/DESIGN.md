# Design

This document describes the interaction between `EtcdCluster` custom resources and other Kubernetes
primitives and gives an overview of the underlying implementation.

## Reconciliation flowchart

```mermaid
flowchart TD
    Start(Start) --> A[Ensure service.]
      A --> AA{Are there any\nendpoints?}
        AA --> |Yes| AAA[Connect to the cluster\nand fetch all statuses.]
          AAA --> |Got some response| AAAA{All reachable\nmembers have the\nsame cluster ID?}
            AAAA --> |Yes| AAAAA{Is cluster\nin quorum?}
              AAAAA --> |Yes| AAAAAA{Are all members \nmanaged by the operator?}
                AAAAAA --> |Yes| AAAAAAA["`
                  Promote any learners.
                  Ensure configmap with initial cluster matching existing members and cluster state=existing.
                  Ensure StatefulSet with replicas = max member ordinal + 1
                `"]
                  AAAAAAA --> |OK| AAAAAAAA{Are all\nmembers healthy?}
                    AAAAAAAA --> |Yes| AAAAAAAAA{Are all STS pods present\nin the member list?}
                      AAAAAAAAA --> |Yes| AAAAAAAAAA{Is the\nEtcdCluster\nsize equal to the\nStatefulSet\nsize?}
                        AAAAAAAAAA -->|Yes| AAAAAAAAAAA[Set cluster\nstatus to ready.]
                          AAAAAAAAAAA --> HappyStop([Stop])

                        AAAAAAAAAA --> |No, desired\nsize larger| AAAAAAAAAAB[Ensure ConfigMap with\ninitial cluster state existing\nand initial cluster URLs\nequal to current cluster\nplus one member, do\n'member add' API call and\nincrement StatefulSet size.]
                          AAAAAAAAAAB --> ScaleUpStop([Stop])

                        AAAAAAAAAA --> |No, desired\nsize smaller| AAAAAAAAAAC[Member remove API\ncall, then decrement\nStatefulSet size\nthen delete PVC.]
                          AAAAAAAAAAC --> ScaleDownStop([Stop])

                        AAAAAAAAAA --> |Etcd replicas=0\nSTS replicas=1| AAAAAAAAAAD[Decrement\nSTS to zero]
                          AAAAAAAAAAD --> ScaleToZeroStop([Stop])

                    AAAAAAAA --> |No| AAAAAAAAB1[On timeout evict member.]
                      AAAAAAAAB1 --> AAAAAAAAB2[Delete PVC, ensure ConfigMap with\nmembers + this one and delete pod.]

                    AAAAAAAAA --> |No| AAAAAAAAB2

                  AAAAAAA -->|Error| AAAAAAAB([Requeue])

                AAAAAA --> |No| AAAAAAB([Not implemented,\nstop.])

              AAAAA --> |No| AAAAAB([Quorum Loss Detected:
              1. Check for temporary issues:
                 - Network partitions
                 - Pod scheduling problems
              2. If temporary, wait for recovery
              3. If permanent:
                 - Alert operators
                 - Document disaster recovery steps
                 - Consider backup restoration])

            AAAA --> |No| AAAAB[Cluster is in\nsplit-brain. Set\nerror status.]
              AAAAB --> AAAABStop([Stop])

          AAA --> |No members\nreached| AAAB{Is the STS\npresent?}
            AAAB --> |Yes| AAABA{"`Does it have the correct pod spec?`"}
              AAABA --> |Yes| AAABAA(["`The statefulset cannot be ready, as the ready and liveness probes must be failing. Hope it becomes ready or wait for user intervention.`"])
              AAABA --> |No| AAABAB["`Patch the podspec`"]

            AAAB --> |No| AAABB(["`Looks like it was deleted with cascade=orphan. Create it again and see what happens`"])

        AA --> |No| AAB{Is the STS\npresent?}
          AAB --> |Yes| AABA{Does it have the\ncorrect pod spec?}
            AABA --> |Yes| AABAA{Is it\nready?}
              AABAA --> |Yes| AABAAA{Then it must have\nspec.replicas==0\n Is EtcdCluster\n.spec.replicas==0?}
                AABAAA --> |Yes| AABAAAA([Cluster successfully\nscaled to zero, stop.])
                AABAAA --> |No| AABAAAB["`
                  Ensure ConfigMap with initial cluster = new,
                  initial cluster peers with single member name-0,
                  increment STS size.
                `"]

              AABAA --> |No| AABAAB([Stop and wait, either\nit will turn ready soon\nand the next reconcile\nwill move things along,\nor user intervention is\nneeded])

            AABA --> |No| AABAB[Patch the podspec]

          AAB --> |No| AABB[Create configmap, initial state new\ninitial cluster according to spec.\nreplicas, create statefulset.]
```
