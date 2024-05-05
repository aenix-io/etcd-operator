package v1alpha1

func IsClientSecurityEnabled(c *EtcdCluster) bool {
	clientSecurityEnabled := false
	if c.Spec.Security != nil && c.Spec.Security.TLS.ClientSecret != "" {
		clientSecurityEnabled = true
	}
	return clientSecurityEnabled
}

func IsServerSecurityEnabled(c *EtcdCluster) bool {
	serverSecurityEnabled := false
	if c.Spec.Security != nil && c.Spec.Security.TLS.ServerSecret != "" {
		serverSecurityEnabled = true
	}
	return serverSecurityEnabled
}

func IsServerCADefined(c *EtcdCluster) bool {
	serverCADefined := false
	if c.Spec.Security != nil && c.Spec.Security.TLS.ServerTrustedCASecret != "" {
		serverCADefined = true
	}
	return serverCADefined
}
