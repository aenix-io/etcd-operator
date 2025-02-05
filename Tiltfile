update_settings(k8s_upsert_timeout_secs=60)  # on first tilt up, often can take longer than 30 seconds

# tilt settings
settings = {
    "allowed_contexts": [
        "kind-etcd-operator-dev"
    ],
    "kubectl": "bin/kubectl",
    "kustomize": "bin/kustomize",
    "cert_manager_version": "v1.15.3",
}

# define variables and functions
base_path = config.main_dir
kubectl_binary = "{}/{}".format(base_path, settings.get("kubectl"))
kustomize_binary = "{}/{}".format(base_path, settings.get("kustomize"))

if "allowed_contexts" in settings:
    allow_k8s_contexts(settings.get("allowed_contexts"))

def deploy_cert_manager():
    version = settings.get("cert_manager_version")
    print("Installing cert-manager")
    local("{} apply -f https://github.com/cert-manager/cert-manager/releases/download/{}/cert-manager.yaml".format(kubectl_binary, version), quiet=True, echo_off=True)

    print("Waiting for cert-manager to start")
    local("{} wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager".format(kubectl_binary), quiet=True, echo_off=True)
    local("{} wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-cainjector".format(kubectl_binary), quiet=True, echo_off=True)
    local("{} wait --for=condition=Available --timeout=300s -n cert-manager deployment/cert-manager-webhook".format(kubectl_binary), quiet=True, echo_off=True)

def prepare_etcd_operator():
    docker_build('ghcr.io/aenix-io/etcd-operator', '.')
    return kustomize("./config/dev", kustomize_bin=kustomize_binary)

# deploy everything
deploy_cert_manager()

yaml = prepare_etcd_operator()
k8s_yaml(yaml)
