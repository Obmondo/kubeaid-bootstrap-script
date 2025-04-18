package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	gitUtils "github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/git"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/kubernetes"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
	argoCDV1Alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SetupClusterArgs struct {
	*CreateDevEnvArgs

	IsManagementCluster bool
	ClusterClient       client.Client

	GitAuthMethod transport.AuthMethod
}

func SetupCluster(ctx context.Context, args SetupClusterArgs) {
	clusterType := constants.ClusterTypeManagement
	if !args.IsManagementCluster {
		clusterType = constants.ClusterTypeMain
	}

	slog.InfoContext(ctx, "Setting up cluster....", slog.String("cluster-type", clusterType))

	{
		gitAuthMethod := args.GitAuthMethod
		// If we're going to use the original KubeAid repo (https://github.com/Obmondo/KubeAid), then we
		// don't need any Git authentication method
		if config.ParsedGeneralConfig.Forks.KubeaidForkURL == constants.RepoURLObmondoKubeAid {
			gitAuthMethod = nil
		}

		// Clone the KubeAid fork locally (if not already cloned).
		kubeAidRepo := gitUtils.CloneRepo(ctx,
			config.ParsedGeneralConfig.Forks.KubeaidForkURL,
			utils.GetKubeAidDir(),
			gitAuthMethod,
		)

		kubeaidVersion := config.ParsedGeneralConfig.Cluster.KubeaidVersion

		if kubeaidVersion != "HEAD" {
			// Hard reset to the KubeAid tag mentioned in the KubeAid Bootstrap Script config file.
			// TODO : Move this to gitUtils.CloneRepo( )?

			slog.InfoContext(ctx, "Hard resetting the KubeAid repo to tag", slog.String("tag", kubeaidVersion))

			kubeAidRepoWorktree, err := kubeAidRepo.Worktree()
			assert.AssertErrNil(ctx, err, "Failed getting KubeAid repo worktree")

			tagReference, err := kubeAidRepo.Reference(plumbing.NewTagReferenceName(kubeaidVersion), true)
			assert.AssertErrNil(ctx, err, "Failed resolving reference for provided tag in KubeAid repo")

			targetCommitHash := tagReference.Hash()

			tagObject, err := kubeAidRepo.TagObject(tagReference.Hash())
			if err == nil {
				// Resolve the tag reference hash to the tag object / corresponding commit hash.
				targetCommitHash = tagObject.Target
			}

			err = kubeAidRepoWorktree.Reset(&git.ResetOptions{
				Commit: targetCommitHash,
				Mode:   git.HardReset,
			})
			assert.AssertErrNil(ctx, err, "Failed hard resetting KubeAid repo to provided tag")
		}
	}

	// Install Sealed Secrets.
	kubernetes.InstallSealedSecrets(ctx)

	// If we're recovering a cluster, then we need to restore the Sealed Secrets controller private
	// keys from a previous cluster which got destroyed.
	if args.IsPartOfDisasterRecovery {
		sealedSecretsKeysBackupBucketName := config.ParsedGeneralConfig.Cloud.AWS.DisasterRecovery.SealedSecretsBackupS3BucketName
		sealedSecretsKeysDirPath := utils.GetDownloadedStorageBucketContentsDir(sealedSecretsKeysBackupBucketName)

		utils.ExecuteCommandOrDie(fmt.Sprintf("kubectl apply -f %s", sealedSecretsKeysDirPath))

		slog.InfoContext(ctx,
			"Restored Sealed Secrets controller private keys from a previous cluster",
			slog.String("dir-path", sealedSecretsKeysDirPath),
		)
	}

	// Setup cluster directory in the user's KubeAid config repo.
	SetupKubeAidConfig(ctx, SetupKubeAidConfigArgs{
		CreateDevEnvArgs: args.CreateDevEnvArgs,
		GitAuthMethod:    args.GitAuthMethod,
	})

	// Install and setup ArgoCD.
	kubernetes.InstallAndSetupArgoCD(ctx, utils.GetClusterDir(), args.ClusterClient)

	// Create the capi-cluster / capi-cluster-<customer-id> namespace, where the 'cloud-credentials'
	// Kubernetes Secret will exist.
	kubernetes.CreateNamespace(ctx, kubernetes.GetCapiClusterNamespace(), args.ClusterClient)

	// Sync the Root, CertManager and Secrets ArgoCD Apps one by one.
	argocdAppsToBeSynced := []string{
		"root",
		"cert-manager",
		"secrets",
	}

	for _, argoCDApp := range argocdAppsToBeSynced {
		kubernetes.SyncArgoCDApp(ctx, argoCDApp, []*argoCDV1Alpha1.SyncOperationResource{})
	}

	// If trying to provision a main cluster in some cloud provider like AWS / Azure / Hetzner.
	if globals.CloudProviderName != constants.CloudProviderLocal {
		// Sync ClusterAPI ArgoCD App.
		kubernetes.SyncArgoCDApp(ctx, "cluster-api", []*argoCDV1Alpha1.SyncOperationResource{})

		// Sync the Infrastructure Provider component of the capi-cluster ArgoCD App.
		// TODO : Use ArgoCD sync waves so that we don't need to explicitly sync the Infrastructure
		//        Provider component first.
		syncInfrastructureProvider(ctx, args.ClusterClient)
	}

	printHelpTextForArgoCDDashboardAccess(clusterType)
}

// Syncs the Infrastructure Provider component of the CAPI Cluster ArgoCD App and waits for the
// infrastructure specific CRDs to be installed and pod to be running.
func syncInfrastructureProvider(ctx context.Context, clusterClient client.Client) {
	// Sync the Infrastructure Provider component.
	kubernetes.SyncArgoCDApp(ctx, constants.ArgoCDAppCapiCluster, []*argoCDV1Alpha1.SyncOperationResource{
		{
			Group: "operator.cluster.x-k8s.io",
			Kind:  "InfrastructureProvider",
			Name:  getInfrastructureProviderName(),
		},
	})

	capiClusterNamespace := kubernetes.GetCapiClusterNamespace()

	// Wait for the infrastructure specific CRDs to be installed and infrastructure provider component
	// pod to be running.

	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("namespace", capiClusterNamespace),
	})

	wait.PollUntilContextCancel(ctx, time.Minute, false, func(ctx context.Context) (bool, error) {
		podList := &coreV1.PodList{}
		err := clusterClient.List(ctx, podList, &client.ListOptions{
			Namespace: capiClusterNamespace,
		})
		assert.AssertErrNil(ctx, err, "Failed listing pods")

		if (len(podList.Items) > 0) && (podList.Items[0].Status.Phase == coreV1.PodRunning) {
			return true, nil
		}

		slog.InfoContext(ctx, "Waiting for the infrastructure provider component pod to come up")
		return false, nil
	})
}

// Returns the name of the InfrastructureProvider component.
func getInfrastructureProviderName() string {
	infrastructureProviderName := globals.CloudProviderName

	if len(config.ParsedGeneralConfig.CustomerID) > 0 {
		infrastructureProviderName = infrastructureProviderName + "-" + config.ParsedGeneralConfig.CustomerID
	}

	return infrastructureProviderName
}

func printHelpTextForArgoCDDashboardAccess(clusterType string) {
	clusterKubeconfigPath := constants.OutputPathManagementClusterHostKubeconfig
	if clusterType == constants.ClusterTypeMain {
		clusterKubeconfigPath = constants.OutputPathMainClusterKubeconfig
	}

	// Print out help text for the user to access ArgoCD admin dashboard.
	helpText := fmt.Sprintf(
		`
Finished setting up %s cluster.

To access the ArgoCD admin dashboard :

  (1) In your host machine's terminal, navigate to the directory from where you executed the
      script (you'll notice the outputs/ directory there). Do :

        export KUBECONFIG=%s

  (2) Retrieve the ArgoCD admin password :

        echo "ArgoCD admin password : "
        kubectl get secret argocd-initial-admin-secret --namespace argocd \
          -o jsonpath="{.data.password}" | base64 -d

  (3) Port forward ArgoCD server :

        kubectl port-forward svc/argocd-server --namespace argocd 8080:443

  (4) Visit https://localhost:8080 in a browser and login to ArgoCD as admin.
    `,
		clusterType,
		clusterKubeconfigPath,
	)
	println(helpText)
}
