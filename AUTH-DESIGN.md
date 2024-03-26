# Authentication, authorization and secure communication

* Status: proposed
* Date: 2024-04-03

Guthub issue: https://github.com/aenix-io/etcd-operator/issues/76

## Security specification

```
kind: EtcdCluster
spec:
...
  security:
    peer:
      caSecretName: peer-ca-tls-secret
      tlsSecretName: peer-server-tls-secret
    clientServer:
      caSecretName: client-server-ca-tls-secret
      tlsSecretName: client-server-server-tls-secret
      auth:
        tlsSecretName: client-server-client-tls-secret
...
```

It is expected that secrets contain sections with specific names: `tls.crt`, `tls.key` for tlsSecret and `ca.crt` for caSecret.

All fields are optional, but if one field is defined in a pair (caSecretName and tlsSecretName), other must be defined as well - it will be validated in a webhook.

## Peer communication
If peer secrets are not defined, then `--peer-auto-tls` option is used that allows etcd to communicate via https.

If peer certificate/key is reissued, etcd cluster does rollout restart to reread the secret. Operator watches these secrets.

One secret is used for all etcd nodes.

## Client-server communication
If client-server secrets are not defined, then `--auto-tls` option is used that allows clients to communicate via https.

If client-server certificate/key is reissued, etcd cluster does rollout restart to reread the secret. Operator watches these secrets.

## User authentication
If enabler is true, user authentication is enabled and `root` user is created in etcd without a password. It is expected that customer provides valid secret for operator authentication (to operate etcd cluster) with `tls.crt` and `tls.key` sections. As multiple secrets for multiple etcd clusters are created on the fly, secrets are not mounted to operator => secrets are read on the fly and reread by operator if certificates are reissued.

If `auth.tlsSecretName` is defined, then the whole `clientServer` section must be defined as well => validated in a webhook.

## Futher improvements to be described and discussed

1. * What: Use separate controller (CR) to create k8s secrets with certificates/passwords and renew them relularly.
   * Why:
     * Etcd clients (apps deployed to k8s) will need to have possibility to access created etcd clusters. It would be inconvenient to couple user lists in EtcdCluster CR (with complete RBAC lists) with users in the application configurations.
2. * What: Remove cert-manager dependency to create and rotate certificates.
   * Why:
     * Openshift has its own ecosystem and doesn't have cert-manager out of the box. It has own operator.
     * Cert-manager dependency (ceparate operator) is too heavy for etcd-operator.
