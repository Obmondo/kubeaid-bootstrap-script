package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/aws"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func BootstrapCluster(ctx context.Context, skipKubeAidConfigSetup, skipClusterctlMove bool, cloudProvider cloud.CloudProvider, isPartOfDisasterRecovery bool) {
	// Detect git authentication method.
	gitAuthMethod := utils.GetGitAuthMethod(ctx)

	// Any cloud specific tasks.
	switch {
	case config.ParsedConfig.Cloud.AWS != nil:
		aws.SetAWSSpecificEnvs()
		aws.CreateIAMCloudFormationStack()
	}

	os.Setenv(constants.EnvNameKubeconfig, constants.OutputPathManagementClusterKubeconfig)

	// Create the management cluster (using K3d), if it doesn't already exist.
	utils.CreateK3DCluster(ctx, "management-cluster")

	// Install Sealed Secrets.
	utils.InstallSealedSecrets(ctx)

	// Setup cluster directory in the user's KubeAid config repo.
	if !skipKubeAidConfigSetup {
		SetupKubeAidConfig(ctx, gitAuthMethod, false)
	}

	// While retrying, if `clusterctl move` has already been executed, then we skip these steps and
	// move to the disaster recovery setup step.
	provisionedClusterClient := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig)
	if !utils.IsClusterctlMoveExecuted(ctx, provisionedClusterClient) {
		// Provision the main cluster
		provisionMainCluster(ctx, gitAuthMethod, skipKubeAidConfigSetup)

		// Let the provisioned cluster manage itself.
		dogfoodProvisionedCluster(ctx, gitAuthMethod, skipClusterctlMove, cloudProvider, isPartOfDisasterRecovery)
	}

	// If the diasterRecovery section is specified in the cloud-provider specific config, then
	// setup Disaster Recovery.
	if config.ParsedConfig.Cloud.AWS.DisasterRecovery != nil {
		cloudProvider.SetupDisasterRecovery(ctx)
	}
}

func provisionMainCluster(ctx context.Context, gitAuthMethod transport.AuthMethod, skipKubeAidConfigSetup bool) {
	managementClusterClient := utils.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig)

	// Setup the management cluster.
	SetupCluster(ctx, managementClusterClient)

	// Sync the complete capi-cluster ArgoCD App.
	// TODO : Make it compatible with the retry feature, when `clusterctl move` is already performed.
	utils.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{})

	// Close ArgoCD application client.
	constants.ArgoCDApplicationClientCloser.Close()

	// CASE : Hetzner
	// Make the Failover IP point to the master node where `kubeadm init` has been executed.
	if config.ParsedConfig.Cloud.Hetzner != nil {
		hetzner.ExecuteFailoverScript(ctx)
	}

	// Wait for the main cluster to be provisioned and ready.
	utils.WaitForMainClusterToBeProvisioned(ctx, managementClusterClient)

	// Save kubeconfig locally.
	utils.SaveKubeconfig(ctx, managementClusterClient)

	slog.Info("Cluster has been provisioned successfully 🎉🎉 !", slog.String("kubeconfig", constants.OutputPathProvisionedClusterKubeconfig))
}

func dogfoodProvisionedCluster(ctx context.Context, gitAuthMethod transport.AuthMethod, skipClusterctlMove bool, cloudProvider cloud.CloudProvider, isPartOfDisasterRecovery bool) {
	// Update the KUBECONFIG environment variable's value to the provisioned cluster's kubeconfig.
	os.Setenv("KUBECONFIG", constants.OutputPathProvisionedClusterKubeconfig)
	provisionedClusterClient := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig)

	// Wait for atleast 1 worker node to be initialized, so that we can deploy our application
	// workloads.
	utils.WaitForMainClusterToBeReady(ctx, provisionedClusterClient)

	if isPartOfDisasterRecovery {
		// If this is a part of the disaster recovery process, then
		// restore Kubernetes Secrets containing a Sealed Secrets key.

		sealedSecretsBackupBucketName := cloudProvider.GetSealedSecretsBackupBucketName()
		manifestsDirPath := utils.GetDirPathForDownloadedStorageBucketContents(sealedSecretsBackupBucketName)

		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", manifestsDirPath))
	}

	// Install Sealed Secrets.
	utils.InstallSealedSecrets(ctx)

	// We need to update the Sealed Secrets in the kubeaid-config fork.
	// Those represent Kubernetes Secrets encyrpted using the private key of the Sealed Secrets
	// controller installed in the K3d management cluster.
	// We need to update them, by encrypting the underlying Kubernetes Secrets using the private
	// key of the Sealed Secrets controller installed in the provisioned main cluster.
	SetupKubeAidConfig(ctx, gitAuthMethod, true)

	// Setup the provisioned cluster.
	SetupCluster(ctx, provisionedClusterClient)

	if !skipClusterctlMove {
		// Make ClusterAPI use IAM roles instead of (temporary) credentials.
		//
		// NOTE : The ClusterAPI AWS InfrastructureProvider component (CAPA controller) needs to run in
		//        a master node.
		//        And, the master node count should be more than 1.
		{
			// Zero the credentials CAPA controller started with.
			// This will force the CAPA controller to fall back to use the attached instance profiles.
			utils.ExecuteCommandOrDie("clusterawsadm controller zero-credentials --namespace capi-cluster")

			// Rollout and restart on capa-controller-manager deployment.
			utils.ExecuteCommandOrDie("clusterawsadm controller rollout-controller --namespace capi-cluster")
		}

		// Move ClusterAPI manifests to the provisioned cluster.
		utils.ExecuteCommandOrDie(fmt.Sprintf(
			"clusterctl move --kubeconfig %s --namespace %s --to-kubeconfig %s",
			constants.OutputPathManagementClusterKubeconfig, utils.GetCapiClusterNamespace(), constants.OutputPathProvisionedClusterKubeconfig,
		))

		// Sync cluster-autoscaler ArgoCD App.
		utils.SyncArgoCDApp(ctx, constants.ArgoCDAppClusterAutoscaler, []*argoCDV1Alpha1.SyncOperationResource{})
	}
}
