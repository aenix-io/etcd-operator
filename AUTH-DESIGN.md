# Authentication, authorization and secure communication

* Status: proposed
* Date: 2024-03-24

Guthub issue: https://github.com/aenix-io/etcd-operator/issues/76
<!--
## Context and Problem Statement

 Etcd-operator should provide clients functionality of secure communication (auth and tls).

## How Kamaji creates certificate infrastructure

[Permalink](https://github.com/clastix/kamaji-etcd/blob/0536a5452d074022edfd5647040d14ee30539aaf/charts/kamaji-etcd/templates/etcd_job_preinstall_1.yaml#L26) to their repo.

```
cfssl gencert -initca /csr/ca-csr.json | cfssljson -bare /certs/ca &&
mv /certs/ca.pem /certs/ca.crt && mv /certs/ca-key.pem /certs/ca.key &&
cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=peer-authentication /csr/peer-csr.json | cfssljson -bare /certs/peer &&
cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=peer-authentication /csr/server-csr.json | cfssljson -bare /certs/server &&
cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=client-authentication /csr/root-client-csr.json | cfssljson -bare /certs/root-client
```

### Generate CA

`cfssl gencert -initca /csr/ca-csr.json | cfssljson -bare /certs/ca` - initiates CA key and certificate. We need to create ca-csr configuration. Kamaji example:
```
{
  "CN": "Clastix CA",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "IT",
      "ST": "Italy",
      "L": "Milan"
    }
  ]
}
```

CSR for our etcd (if we need it with cert-manager):
```
{
  "CN": "AEnix etcd cluster <<cluster-name>> CA",
  "key": {
    "algo": "rsa",
    "size": 4096
  },
  "names": [
    {
      "C": "?",
      "ST": "?",
      "L": "?"
    }
  ]
}
```
Important points:
* Reissue CA certificate automatically.
* Size 4096 bytes.
* We can't rotate CA key. If CA key is rotated, then all client and peer certificates must be signed by new CA key at the same time => possible long downtime. Old certificates signed by old CA key will not work.
* Different CAs for peer and client authentication. Some customers will need to issue their own certificates for clients applications and will not want to touch peer certificates.
* We should not place ca.key to etcd-cluster namespace and mount it to etcd cluster (kamaji mounts it to etcd-cluster, defrag and backup jobs). Ca.key is necessary only for issuing peer and client certificates - can be placed to etcd-operator namespace and must be protected.
* Separate CA for every etcd cluster.
  * Why: Want to make etcd-clusters isolated. No need for different etcd clusters to have common CA.
  * Possible risk: Will applications be able to use different certificates to different etcd clusters if they need? Do libraries allow that?

Questions:
* How to fill C, ST, L fields? Are they mandatory?

### Generate certificate for peer authentication
Peer authentication certs authenticate etcd nodes against etcd nodes and used as server certificates.

`cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=peer-authentication /csr/peer-csr.json | cfssljson -bare /certs/peer`

config.json - configuration for certificate requests (1 year validity):
```
{
  "signing": {
    "default": {
      "expiry": "8760h"
    },
    "profiles": {
      "server-authentication": {
        "usages": ["signing", "key encipherment", "server auth"],
        "expiry": "8760h"
      },
      "client-authentication": {
        "usages": ["signing", "key encipherment", "client auth"],
        "expiry": "8760h"
      },
      "peer-authentication": {
        "usages": ["signing", "key encipherment", "server auth", "client auth"],
        "expiry": "8760h"
      }
    }
  }
}
```

peer-csr.json - certificate request config. We need to create it. Kamaji example:
```
{
  "CN": "etcd",
  "key": {
    "algo": "rsa",
    "size": 4096
  },
  "hosts": ["test-test-etcd-0",
    "test-test-etcd-0.test-test-etcd",
    "test-test-etcd-0.test-test-etcd.etcd-operator-system.svc",
    "test-test-etcd-0.test-test-etcd.etcd-operator-system.svc.cluster.local","test-test-etcd-1",
    "test-test-etcd-1.test-test-etcd",
    "test-test-etcd-1.test-test-etcd.etcd-operator-system.svc",
    "test-test-etcd-1.test-test-etcd.etcd-operator-system.svc.cluster.local","test-test-etcd-2",
    "test-test-etcd-2.test-test-etcd",
    "test-test-etcd-2.test-test-etcd.etcd-operator-system.svc",
    "test-test-etcd-2.test-test-etcd.etcd-operator-system.svc.cluster.local",
    "127.0.0.1"
  ]
}
```

* test-test-etcd - helm release name
* etcd-operator-system - namespace name
* 3 replicas

Hostnames helm template:
```
{{- range $count := until (int $.Values.replicas) -}}
    {{ printf "\"%s-%d\"," ( include "etcd.stsName" $outer ) $count }}
    {{ printf "\"%s-%d.%s\"," ( include "etcd.stsName" $outer ) $count (include "etcd.serviceName" $outer) }}
    {{ printf "\"%s-%d.%s.%s.svc\"," ( include "etcd.stsName" $outer ) $count (include "etcd.serviceName" $outer) $.Release.Namespace }}
    {{ printf "\"%s-%d.%s.%s.svc.cluster.local\"," ( include "etcd.stsName" $outer ) $count (include "etcd.serviceName" $outer) $.Release.Namespace }}
{{- end }}
```

Important points:
* Rotate private keys. They are not rotated by cert-manager by default. If we enable this feature, private key and certificate will be written at the same time => no downtime is expected. See [cert-manager docs](https://cert-manager.io/docs/usage/certificate/#issuance-behavior-rotation-of-the-private-key).

Questions:
* Key length? It is not recommended to use 2048, but 4096 can bring higher CPU usage. 3072? Cert-manager does not recreate private key if key length setting is changed. **It was true before - most probably now length can be changed on-the-fly.**
* Expiration period? Hardcoded 1 year for the first implementation (30 days reissue before expiration).

### Generate server certificate for client trust
`cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=peer-authentication /csr/server-csr.json | cfssljson -bare /certs/server`

 server-csr.json - certificate request config. We need to create it. Kamaji example:
 ```
 {
  "CN": "etcd",
  "key": {
    "algo": "rsa",
    "size": 4096
  },
  "hosts": ["test-test-etcd-0.test-test-etcd.etcd-operator-system.svc.cluster.local","test-test-etcd-1.test-test-etcd.etcd-operator-system.svc.cluster.local","test-test-etcd-2.test-test-etcd.etcd-operator-system.svc.cluster.local",
    "etcd-server.etcd-operator-system.svc.cluster.local",
    "etcd-server.etcd-operator-system.svc",
    "etcd-server",
    "127.0.0.1"
  ]
}
```

Hostnames helm template:
```
{{- range $count := until (int $.Values.replicas) -}}
    {{ printf "\"%s-%d.%s.%s.svc.cluster.local\"," ( include "etcd.fullname" $outer ) $count (include "etcd.serviceName" $outer) $.Release.Namespace }}
{{- end }}
```

Important points:
* Rotate private keys. They are not rotated by cert-manager by default. If we enable this feature, private key and certificate will be written at the same time => no downtime is expected. See [cert-manager docs](https://cert-manager.io/docs/usage/certificate/#issuance-behavior-rotation-of-the-private-key).

Questions:
* Key length? It is not recommended to use 2048, but 4096 can bring higher CPU usage. 3072? Cert-manager does not recreate private key if key length setting is changed. **It was true before - most probably now length can be changed on-the-fly.**
* Expiration period? Hardcoded 1 year for the first implementation (30 days reissue before expiration).

### Generate root client certificate

`cfssl gencert -ca=/certs/ca.crt -ca-key=/certs/ca.key -config=/csr/config.json -profile=client-authentication /csr/root-client-csr.json | cfssljson -bare /certs/root-client`

root-client-csr.json request config. We need to create it. Kamaji example:
```
{
  "CN": "root",
  "key": {
    "algo": "rsa",
    "size": 4096
  },
  "names": [
    {
      "O": "system:masters"
    }
  ]
}
```

Important points:
* Root certificate will be used only by operator for maintenance tasks. Certificate for Kubernetes is generated separately.
* Rotate private keys. They are not rotated by cert-manager by default. If we enable this feature, private key and certificate will be written at the same time => no downtime is expected. See [cert-manager docs](https://cert-manager.io/docs/usage/certificate/#issuance-behavior-rotation-of-the-private-key).
* Root client certificate can be used for kubernetes api server application during the beta test period.

Questions:
* Key length? It is not recommended to use 2048, but 4096 can bring higher CPU usage. 3072? Cert-manager does not recreate private key if key length setting is changed. **It was true before - most probably now length can be changed on-the-fly.**
* Expiration period? Hardcoded 1 year for the first implementation (30 days reissue before expiration).



## [Etcd reference for v3.5](https://etcd.io/docs/v3.5/op-guide/security/)

What certificates and private keys to use for what configuration key: [kamaji-etcd chart](https://github.com/clastix/kamaji-etcd/blob/0536a5452d074022edfd5647040d14ee30539aaf/charts/kamaji-etcd/templates/etcd_sts.yaml#L49):

Client-to-server communication:

`--cert-file=<path>`: Certificate used for SSL/TLS connections to etcd. When this option is set, advertise-client-urls can use the HTTPS schema.

`--key-file=<path>`: Key for the certificate. Must be unencrypted.

`--client-cert-auth`: When this is set etcd will check all incoming HTTPS requests for a client certificate signed by the trusted CA, requests that don’t supply a valid client certificate will fail. If authentication is enabled, the certificate provides credentials for the user name given by the Common Name field.

`--trusted-ca-file=<path>`: Trusted certificate authority.

`--auto-tls`: Use automatically generated self-signed certificates for TLS connections with clients.

Peer (server-to-server / cluster) communication:

The peer options work the same way as the client-to-server options:

`--peer-cert-file=<path>`: Certificate used for SSL/TLS connections between peers. This will be used both for listening on the peer address as well as sending requests to other peers.

`--peer-key-file=<path>`: Key for the certificate. Must be unencrypted.

`--peer-client-cert-auth`: When set, etcd will check all incoming peer requests from the cluster for valid client certificates signed by the supplied CA.

`--peer-trusted-ca-file=<path>`: Trusted certificate authority.

`--peer-auto-tls`: Use automatically generated self-signed certificates for TLS connections between peers.

If either a client-to-server or peer certificate is supplied the key must also be set. All of these configuration options are also available through the environment variables, ETCD_CA_FILE, ETCD_PEER_CA_FILE and so on.

Common options:

`--cipher-suites`: Comma-separated list of supported TLS cipher suites between server/client and peers (empty will be auto-populated by Go).

`--tls-min-version=<version>` Sets the minimum TLS version supported by etcd.

`--tls-max-version=<version>` Sets the maximum TLS version supported by etcd. If not set the maximum version supported by Go will be used. -->

## Futher improvements to be described and discussed

1. * What: Use separate controller (CR) to create k8s secrets with certificates/passwords and renew them relularly.
   * Why:
     * Etcd clients (apps deployed to k8s) will need to have possibility to access created etcd clusters. It would be inconvenient to couple user lists in EtcdCluster CR (with complete RBAC lists) with users in the application configurations.
2. * What: Remove cert-manager dependency to create and rotate certificates.
   * Why:
     * Openshift has its own ecosystem and doesn't have cert-manager out of the box. It has own operator.
     * Cert-manager dependency (ceparate operator) is too heavy for etcd-operator.
