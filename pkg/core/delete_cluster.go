package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils"
	"github.com/Obmondo/kubeaid-bootstrap-script/utils/assert"
	"github.com/avast/retry-go/v4"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterAPIV1Beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DeleteCluster(ctx context.Context) {
	cluster := &clusterAPIV1Beta1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      config.ParsedConfig.Cluster.Name,
			Namespace: utils.GetCapiClusterNamespace(),
		},
	}

	provisionedClusterClient := utils.CreateKubernetesClient(ctx, constants.OutputPathProvisionedClusterKubeconfig)

	// The Cluster resource exists in the provisioned cluster.
	// The means, the 'clusterctl move' command has been executed.
	if utils.IsClusterctlMoveExecuted(ctx, provisionedClusterClient) {
		slog.InfoContext(ctx, "Detected that the 'clusterctl move' command has been executed")

		// Move back the ClusterAPI manifests back from the provisioned cluster to the management
		// cluster.
		// NOTE : We need to retry, since we can get 'failed to call webhook' error sometimes.
		retry.Do(func() error {
			_, err := utils.ExecuteCommand(fmt.Sprintf(
				"clusterctl move --kubeconfig %s --to-kubeconfig %s -n %s",
				constants.OutputPathProvisionedClusterKubeconfig, constants.OutputPathManagementClusterKubeconfig, utils.GetCapiClusterNamespace(),
			))
			return err
		})
	}

	managementClusterClient := utils.CreateKubernetesClient(ctx, constants.OutputPathManagementClusterKubeconfig)

	// Get the Cluster resource from the management cluster.
	err := utils.GetClusterResource(ctx, managementClusterClient, cluster)
	assert.AssertErrNil(ctx, err, "Cluster resource was suppossed to be present in the management cluster")

	// If the cluster gets marked as paused, then unmark it first.
	if cluster.Spec.Paused {
		err := managementClusterClient.Update(ctx, cluster)
		assert.AssertErrNil(ctx, err, "Failed unmarking paused cluster")
	}

	// Delete the Cluster resource from the management cluster.
	// This will cause the actual provisioned cluster to be deleted.

	clusterDeletionTimeout := 10 * time.Minute.Milliseconds() // (10 minutes)
	err = managementClusterClient.Delete(ctx, cluster, &client.DeleteOptions{
		GracePeriodSeconds: &clusterDeletionTimeout,
	})
	assert.AssertErrNil(ctx, err, "Failed deleting cluster")

	// Wait for the infrastructure to be destroyed.
	wait.PollUntilContextCancel(ctx, 2*time.Minute, false, func(ctx context.Context) (bool, error) {
		slog.InfoContext(ctx, "Waiting for cluster infrastructure to be destroyed")

		err := managementClusterClient.Get(ctx, types.NamespacedName{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		}, cluster)
		isInfrastructureDeleted := errors.IsNotFound(err)
		return isInfrastructureDeleted, nil
	})

	slog.InfoContext(ctx, "Deleted cluster successully")
}
