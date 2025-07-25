package core

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"time"

	yqCmd "github.com/mikefarah/yq/v4/cmd"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	kcpV1Beta1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

type (
	UpgradeClusterArgs struct {
		NewKubernetesVersion string

		CloudSpecificUpdates any
	}
)

func UpgradeCluster(ctx context.Context, skipPRFlow bool, args UpgradeClusterArgs) {
	// Update the values-capi-cluster.yaml file in the kubeaid-config repo.
	updateCapiClusterValuesFile(ctx, args)

	// Construct the Kubernetes (management / provisioned) cluster client.
	var clusterClient client.Client
	{
		clusterClient = kubernetes.MustCreateClusterClient(ctx,
			constants.OutputPathMainClusterKubeconfig,
		)

		// If 'clusterctl move' wasn't executed, then we need to communicate with the management
		// cluster instead.
		if !kubernetes.IsClusterctlMoveExecuted(ctx) {
			clusterClient = kubernetes.MustCreateClusterClient(ctx,
				kubernetes.GetManagementClusterKubeconfigPath(ctx),
			)
		}
	}

	// (1) Upgrading the Control Plane.
	upgradeControlPlane(ctx, args, clusterClient)

	// (2) Upgrading each node-group one by one.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		for _, nodeGroup := range config.ParsedGeneralConfig.Cloud.AWS.NodeGroups {
			upgradeNodeGroup(ctx, args, clusterClient, nodeGroup.Name)
		}

	default:
		panic("unimplemented")
	}
}

// Update the values-capi-cluster.yaml file in the kubeaid-config repo.
// Once the changes get merged, only then we'll trigger the actual rollout process.
func updateCapiClusterValuesFile(ctx context.Context, args UpgradeClusterArgs) {
	// Detect git authentication method.
	gitAuthMethod := git.GetGitAuthMethod(ctx)

	// Clone the KubeAid config fork locally (if not already cloned).
	repo := git.CloneRepo(ctx,
		config.ParsedGeneralConfig.Forks.KubeaidConfigForkURL,
		utils.GetKubeAidConfigDir(),
		gitAuthMethod,
	)

	workTree, err := repo.Worktree()
	assert.AssertErrNil(ctx, err, "Failed getting worktree")

	// Create and checkout to a new branch.
	newBranchName := fmt.Sprintf(
		"kubeaid-%s-%d",
		config.ParsedGeneralConfig.Cluster.Name,
		time.Now().Unix(),
	)
	git.CreateAndCheckoutToBranch(ctx, repo, newBranchName, workTree, gitAuthMethod)

	// Update values-capi-cluster.yaml file (using yq).
	{
		capiClusterValuesFilePath := path.Join(
			utils.GetClusterDir(),
			"argocd-apps/values-capi-cluster.yaml",
		)

		// Update Kubernetes version.
		yqCmd := yqCmd.New()
		yqCmd.SetArgs([]string{
			"--in-place", "--yaml-output", "--yaml-roundtrip",

			fmt.Sprintf("'(.global.kubernetes.version) = \"%s\"'", args.NewKubernetesVersion),

			capiClusterValuesFilePath,
		})
		err := yqCmd.ExecuteContext(ctx)
		assert.AssertErrNil(ctx, err,
			"Failed updating Kubernetes version in values-capi-cluster.yaml file",
		)

		// Update with cloud-specific details.
		globals.CloudProvider.UpdateCapiClusterValuesFileWithCloudSpecificDetails(ctx,
			capiClusterValuesFilePath,
			args.CloudSpecificUpdates,
		)
	}

	// Add, commit and push the changes.
	commitMessage := fmt.Sprintf(
		"(cluster/%s) : updated values-capi-cluster.yaml for Kubernetes version upgrade to %s",
		config.ParsedGeneralConfig.Cluster.Name, args.NewKubernetesVersion,
	)
	commitHash := git.AddCommitAndPushChanges(ctx,
		repo,
		workTree,
		newBranchName,
		gitAuthMethod,
		config.ParsedGeneralConfig.Cluster.Name,
		commitMessage,
	)

	// The user now needs to go ahead and create a PR from the new to the default branch. Then he
	// needs to merge that branch.
	// We can't create the PR for the user, since PRs are not part of the core git lib. They are
	// specific to the git platform the user is on.

	// Wait until the PR gets merged.
	defaultBranchName := git.GetDefaultBranchName(ctx, gitAuthMethod, repo)
	git.WaitUntilPRMerged(ctx, repo, defaultBranchName, commitHash, gitAuthMethod, newBranchName)
}

func upgradeControlPlane(
	ctx context.Context,
	args UpgradeClusterArgs,
	clusterClient client.Client,
) {
	slog.InfoContext(ctx, "Triggering Kubernetes version upgrade for the control plane....")

	// Delete and recreate the corresponding machine template with updated options (like AMI in
	// case of AWS).
	// NOTE : Since machine templates are immutable, we cannot directly update them.
	//
	// REFER : https://cluster-api.sigs.k8s.io/tasks/upgrading-clusters#upgrading-the-control-plane-machines.
	globals.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)
	slog.InfoContext(ctx,
		"Recreated the AWSMachineTemplate resource used by the KubeadmControlPlane resource",
	)

	// Update the Kubernetes version in the KubeadmControlPlane resource.

	kubeadmControlPlaneName := fmt.Sprintf(
		"%s-control-plane",
		config.ParsedGeneralConfig.Cluster.Name,
	)

	kubeadmControlPlane := &kcpV1Beta1.KubeadmControlPlane{
		ObjectMeta: v1.ObjectMeta{
			Name:      kubeadmControlPlaneName,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, kubeadmControlPlane)
	assert.AssertErrNil(ctx, err, "Failed retrieving KubeadmControlPlane")

	kubeadmControlPlane.Spec.Version = args.NewKubernetesVersion

	err = clusterClient.Update(ctx, kubeadmControlPlane, &client.UpdateOptions{})
	assert.AssertErrNil(ctx, err, "Failed updating Kubernetes version in KubeadmControlPlane")

	// Ensure that changes to the control-plane start to roll out immediately.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"clusterctl alpha rollout restart kubeadmcontrolplane/%s -n %s",
		kubeadmControlPlane.GetName(), kubeadmControlPlane.GetNamespace(),
	))
}

func upgradeNodeGroup(ctx context.Context,
	args UpgradeClusterArgs,
	clusterClient client.Client,
	nodeGroupName string,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("node-group", nodeGroupName),
	})

	slog.InfoContext(ctx, "Triggering Kubernetes version upgrade for node-group....")

	// Delete and recreate the corresponding machine template with updated options.
	globals.CloudProvider.UpdateMachineTemplate(ctx, clusterClient, args.CloudSpecificUpdates)

	// Update the corresponding MachineDeployment.

	machineDeploymentName := fmt.Sprintf(
		"%s-%s",
		config.ParsedGeneralConfig.Cluster.Name,
		nodeGroupName,
	)

	machineDeployment := &clusterAPIV1Beta1.MachineDeployment{
		ObjectMeta: v1.ObjectMeta{
			Name:      machineDeploymentName,
			Namespace: kubernetes.GetCapiClusterNamespace(),
		},
	}
	err := kubernetes.GetKubernetesResource(ctx, clusterClient, machineDeployment)
	assert.AssertErrNil(ctx, err, "Failed retrieving MachineDeployment resource")

	machineDeployment.Spec.Template.Spec.Version = &args.NewKubernetesVersion

	err = clusterClient.Update(ctx, machineDeployment, &client.UpdateOptions{})
	assert.AssertErrNil(ctx, err, "Failed updating Kubernetes version in MachineDeployment")

	// Ensure that changes to the node-group start to roll out immediately.
	utils.ExecuteCommandOrDie(fmt.Sprintf(
		"clusterctl alpha rollout restart machinedeployment/%s -n %s",
		machineDeployment.GetName(), machineDeployment.GetNamespace(),
	))
}
