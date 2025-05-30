# KubeAid Bootstrap

Welcome to the KubeAid Bootstrap repository! This project aims to simplify the process of setting up a Kubernetes (K8s) cluster across multiple cloud providers. With KubeAid Bootstrap, you can get your cluster up and running in just 10 minutes, complete with essential tools like Keycloak, ArgoCD, Cert-Manager, and monitoring via Kube-Prometheus.

## Features

- **Multi-Cloud Support**: Easily deploy on AWS (Self-managed, EKS), Azure (AKS and self-managed), Hetzner (Cloud, Robot, and Hybrid), and local environments.
- **Latest Kubernetes Version**: Always set up the latest stable version of Kubernetes.
- **GitOps Workflow**: Manage your cluster configuration and deployments using GitOps principles.
- **Integrated Tools**:
  - **Keycloak**: For identity and access management.
  - **ArgoCD**: For continuous delivery and GitOps.
  - **Cert-Manager**: For managing TLS certificates.
  - **Kube-Prometheus**: For monitoring and alerting.

## Getting Started

Follow these steps to set up your Kubernetes cluster:

### Prerequisites

- A cloud account for your chosen provider (AWS, Azure, Hetzner).
- `kubectl` installed on your local machine.
- `git` installed on your local machine.
- `docker` installed on your local machine.
- `jsonnet` installed on your local machine.
- `kubeseal` installed on your local machine.
- `k3d` installed on your local machine.
- Access to a terminal or command line interface.

NOTE: You can also run the ./scripts/install-runtime-dependencies.sh

- A git repo to hold your custom settings for your cluster, [kubeaid-config](https://github.com/Obmondo/kubeaid-config)

### Quick Setup

1. **Generate the [GitHub token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token)**

2. **Download the compose file**:
   - Get the compose file
   ```bash
   wget https://raw.githubusercontent.com/Obmondo/kubeaid-bootstrap-script/refs/heads/main/docker-compose.yaml
   ```

3. **Generate the config**:
   - Run the compose to generate the config, it will drop the file in **/outputs/config**
   ```bash
   docker compose run bootstrap-generate
   ```

4. **Fix the config based on your requirements**:
   - Edit general.yaml
   ```yaml
   forkURLs:
     kubeaidConfig: https://github.com/xxxxxxxx/kubeaid-config.git
   ```

5. **Add the git username and token**:
   - Edit secret.yaml
   ```yaml
   git:
     username: xxxxxxxxxx
     password: xxxxxxxxxx
   ```

6. **Choose your provider [here](https://github.com/Obmondo/kubeaid-bootstrap-script/tree/main?tab=readme-ov-file#cloud-provider-support)**

## Cloud Provider Support

- **AWS**: Self-managed and EKS
  - Documentation: [docs/aws.md](docs/aws.md)

- **Azure**: AKS and self-managed
  - Documentation: [docs/azure.md](docs/azure.md)

- **Hetzner**: Cloud, Robot, and Hybrid
  - Documentation: [docs/hetzner.md](docs/hetzner.md)

- **Local**: Minikube or other local setups
  - Documentation: [docs/local.md](docs/local.md)

## Contributing

We welcome contributions!
Guidelines coming soon!!

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Support

If you encounter any issues or have questions, please open an issue in the GitHub repository or reach out to the community.

---

Get your Kubernetes cluster up and running in no time with KubeAid Bootstrap! Happy clustering! 🚀
