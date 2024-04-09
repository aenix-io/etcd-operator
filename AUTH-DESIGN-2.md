### Peer.ca
| Option | Description |
| ------ | ----------- |
| secretName  | Secret name of user-provided secret. If not specified then operator generates certificate by the spec below |
| metadata    | Metadata of generated secret. |
| duration    | Expiration time of generated secret. |
| renewBefore | Time period before expiration time when certificate will be reissued. |
| privateKey  | Private key configuration: algorithm and key size. |

### Peer.cert
| Option | Description |
| ------ | ----------- |
| secretName  | Secret name of user-provided secret. If not specified then operator generates certificate by the spec below. If peer.ca.secretName is provided, then this certificate is generated from the CA that was provided by the user. You can't define the secret name in this section and do not define peer.ca.secretName. |
| metadata    | Metadata of generated secret. |
| duration    | Expiration time of generated secret. |
| renewBefore | Time period before expiration time when certificate will be reissued. |
| privateKey  | Private key configuration: algorithm, key size and boolean parameter is it necessary to rotate private key when certificate is expired  |

### ClientServer section has the same fields as peer section.

### Rbac
| Option | Description |
| ------ | ----------- |
| enabled  | Enables role-based access control: creates root user in etcd, gives him root role and enables authentication in etcd. |

```yaml
spec:
  security:
    peer:
      enabled: true # optional
      ca:
        # if not defined, then operator generates CA by the spec below
        secretName: ext-peer-ca-tls-secret
        secretTemplate:
          metadata:
            name: peer-ca-tls-secret # optional
            annotations: {} # optional
            labels: {} # optional
        duration: 86400h # optional
        renewBefore: 720h # optional
        privateKey:
          algorithm: RSA # optional
          size: 4096 # optional
      cert:
        secretName: ext-peer-tls-secret
        secretTemplate:
          metadata:
            name: peer-tls-secret # optional
            annotations: {} # optional
            labels: {} # optional
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true # optional
          algorithm: RSA
          size: 4096
    clientServer:
      enabled: true
      ca:
        secretName: ext-server-ca-tls-secret
        secretTemplate:
          metadata:
            name: server-ca-tls-secret
            annotations: {} # optional
            labels: {} # optional
        duration: 86400h
        renewBefore: 720h
        privateKey:
          algorithm: RSA
          size: 4096
      cert:
        secretName: ext-server-tls-secret
        secretTemplate:
          metadata:
            name: server-tls-secret
            annotations: {} # optional
            labels: {} # optional
        extraSans: []
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true
          algorithm: RSA
          size: 4096
      rootClientCert:
        secretName: ext-client-tls-secret
        secretTemplate:
          metadata:
            name: client-tls-secret
            annotations: {} # optional
            labels: {} # optional
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true
          algorithm: RSA
          size: 4096
      rbac:
        enabled: true # optional
```

Important points:
* If field has a value and it is optional, then this value is a default.
* peer:
  * If ca.secretName is not defined, operator generates its own CA.
  * If ca.secretName is defined, then every field under secretName should not be defined.
  * If cert.secretName id not defined, then certificate is generate by operator from the CA defined in the section above (user-managed or operator-managed).
  * User must define ca.secretName if cert.secretName is defined.
  * Algorithm is a list of the values. NOTE: look into the lib that generates certs what values exist (or to cert-manager).
* clientServer:
  * See peer logic.
  * RootClientCert uses server ca and has the same logic as server.cert.
  * Rbac.enabled enables role-based access control: creates root user in etcd, gives him root role and enables authentication in etcd.



security:
  peerCertificate: {}
  peerTrustedCACertficate: {}
  clientCertificate: {}
  serverCertificate: {}
  trustedCACertificate: {}

```yaml
spec:
  security:
    disableClientAuth: false
    peerCertificate:
      secretName: ext-peer-tls-secret
      secretTemplate:
        metadata:
          name: peer-tls-secret
          annotations: {}
          labels: {}
      duration: 720h
      renewBefore: 180h
      privateKey:
        rotate: true # optional
        algorithm: RSA
        size: 4096
    peerTrustedCaCertficate:
      # if not defined, then operator generates CA by the spec below
      secretName: ext-peer-ca-tls-secret
      secretTemplate:
        metadata:
          name: peer-ca-tls-secret
          annotations: {} # optional
          labels: {} # optional
      duration: 86400h # optional
      renewBefore: 720h # optional
      privateKey:
        algorithm: RSA # optional
        size: 4096 # optional
    serverCertificate:
      secretName: ext-server-tls-secret
      secretTemplate:
        metadata:
          name: server-tls-secret
          annotations: {}
          labels: {}
      extraClientSans: []
      duration: 720h
      renewBefore: 180h
      privateKey:
        rotate: true
        algorithm: RSA
        size: 4096
    trustedCaCertificate:
      secretName: ext-server-ca-tls-secret
      secretTemplate:
        metadata:
          name: server-ca-tls-secret
          annotations: {}
          labels: {}
      duration: 86400h
      renewBefore: 720h
      privateKey:
        algorithm: RSA
        size: 4096
    clientCertificate:
      secretName: ext-client-tls-secret
      secretTemplate:
        metadata:
          name: client-tls-secret
          annotations: {}
          labels: {}
      duration: 720h
      renewBefore: 180h
      privateKey:
        rotate: true
        algorithm: RSA
        size: 4096
```






### Peer.ca
| Option | Description |
| ------ | ----------- |
| secretName  | Secret name of user-provided secret. If not specified then operator generates certificate by the spec below |
| metadata    | Metadata of generated secret. |
| duration    | Expiration time of generated secret. |
| renewBefore | Time period before expiration time when certificate will be reissued. |
| privateKey  | Private key configuration: algorithm and key size. |

### Peer.cert
| Option | Description |
| ------ | ----------- |
| secretName  | Secret name of user-provided secret. If not specified then operator generates certificate by the spec below. If peer.ca.secretName is provided, then this certificate is generated from the CA that was provided by the user. You can't define the secret name in this section and do not define peer.ca.secretName. |
| metadata    | Metadata of generated secret. |
| duration    | Expiration time of generated secret. |
| renewBefore | Time period before expiration time when certificate will be reissued. |
| privateKey  | Private key configuration: algorithm, key size and boolean parameter is it necessary to rotate private key when certificate is expired  |

### ClientServer section has the same fields as peer section.

### Rbac
| Option | Description |
| ------ | ----------- |
| enabled  | Enables role-based access control: creates root user in etcd, gives him root role and enables authentication in etcd. |

```yaml
spec:
  security:
    peer:
      enabled: true # optional
      ca:
        # if not defined, then operator generates CA by the spec below
        secretName: ext-peer-ca-tls-secret # oneof secretName or secretTemplate
        secretTemplate: # oneof secretName or secretTemplate
          annotations: {} # optional
          labels: {} # optional
        duration: 86400h # optional
        renewBefore: 720h # optional
        privateKey:
          algorithm: RSA # optional
          size: 4096 # optional
      cert:
        secretName: ext-peer-tls-secret
        secretTemplate:
          annotations: {}
          labels: {}
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true # optional
          algorithm: RSA
          size: 4096
    server:
      enabled: true
      ca:
        secretName: ext-server-ca-tls-secret
        secretTemplate:
          annotations: {}
          labels: {}
        duration: 86400h
        renewBefore: 720h
        privateKey:
          algorithm: RSA
          size: 4096
      cert:
        secretName: ext-server-tls-secret
        secretTemplate:
          annotations: {}
          labels: {}
        extraSANs: []
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true
          algorithm: RSA
          size: 4096
    client:
      enabled: true
      ca:
        secretName: ext-server-ca-tls-secret
        secretTemplate:
          annotations: {}
          labels: {}
        duration: 86400h
        renewBefore: 720h
        privateKey:
          algorithm: RSA
          size: 4096
      cert:
        secretName: ext-client-tls-secret
        secretTemplate:
          annotations: {}
          labels: {}
        duration: 720h
        renewBefore: 180h
        privateKey:
          rotate: true
          algorithm: RSA
          size: 4096
      auth:
        enabled: true # optional
```

Important points:
* If field has a value and it is optional, then this value is a default.
* peer:
  * If ca.secretName is not defined, operator generates its own CA.
  * If ca.secretName is defined, then every field under secretName should not be defined.
  * If cert.secretName id not defined, then certificate is generate by operator from the CA defined in the section above (user-managed or operator-managed).
  * User must define ca.secretName if cert.secretName is defined.
  * Algorithm is a list of the values. NOTE: look into the lib that generates certs what values exist (or to cert-manager).
* clientServer:
  * See peer logic.
  * RootClientCert uses server ca and has the same logic as server.cert.
  * Rbac.enabled enables role-based access control: creates root user in etcd, gives him root role and enables authentication in etcd.

