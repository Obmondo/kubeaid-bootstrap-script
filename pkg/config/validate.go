package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	validatorV10 "github.com/go-playground/validator/v10"
	goNonStandardValidtors "github.com/go-playground/validator/v10/non-standard/validators"
	labelsPkg "github.com/siderolabs/talos/pkg/machinery/labels"
	"golang.org/x/crypto/ssh"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Validates the parsed general and secrets config.
func validateConfigs() {
	ctx := context.Background()

	validator := validatorV10.New(validatorV10.WithRequiredStructEnabled())
	err := validator.RegisterValidation("notblank", goNonStandardValidtors.NotBlank)
	assert.AssertErrNil(ctx, err, "Failed registering notblank validator")

	// Validate based on struct tags.
	{
		err = validator.Struct(ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Struct validation failed for general config")

		err = validator.Struct(ParsedSecretsConfig)
		assert.AssertErrNil(ctx, err, "Struct validation failed for secrets config")
	}

	// Validate that cloud provider credentials have been provided.
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		assert.Assert(ctx, (ParsedSecretsConfig.AWS != nil), "AWS credentials not provided")

	case constants.CloudProviderAzure:
		assert.Assert(ctx, (ParsedSecretsConfig.Azure != nil), "Azure credentials not provided")

	case constants.CloudProviderHetzner:
		assert.Assert(ctx,
			(ParsedSecretsConfig.Hetzner != nil),
			"Hetzner credentials not provided",
		)
	}

	// Validate K8s version.
	ValidateK8sVersion(ctx, ParsedGeneralConfig.Cluster.K8sVersion)

	// Validate additional users.
	for _, additionalUser := range ParsedGeneralConfig.Cluster.AdditionalUsers {
		// Additional user name cannot be ubuntu.
		assert.Assert(ctx, additionalUser.Name != "ubuntu", "additional user name cannot be ubuntu")

		// Validate the public SSH key.
		_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(additionalUser.SSHPublicKey))
		assert.AssertErrNil(ctx, err,
			"SSH public key is invalid : failed parsing",
			slog.String("additional-user", additionalUser.Name),
		)
	}

	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		for _, awsNodeGroup := range ParsedGeneralConfig.Cloud.AWS.NodeGroups {
			validateNodeGroup(ctx, &awsNodeGroup.NodeGroup)
		}

	case constants.CloudProviderAzure:
		for _, azureNodeGroup := range ParsedGeneralConfig.Cloud.Azure.NodeGroups {
			validateNodeGroup(ctx, &azureNodeGroup.NodeGroup)
		}

	case constants.CloudProviderHetzner:
		break

	case constants.CloudProviderLocal:
		break

	default:
		panic("unreachable")
	}
}

func validateNodeGroup(ctx context.Context, nodeGroup *NodeGroup) {
	// Validate auto-scaling options.
	assert.Assert(ctx,
		nodeGroup.MinSize <= nodeGroup.Maxsize,
		"replica count should be <= its max-size",
		slog.String("node-group", nodeGroup.Name),
	)

	// Validate labels and taints.
	validateLabelsAndTaints(ctx, nodeGroup.Name, nodeGroup.Labels, nodeGroup.Taints)
}

// Checks whether the given string represents a valid  and supported Kubernetes version or not.
// If not, then panics.
func ValidateK8sVersion(ctx context.Context, k8sVersion string) {
	parsedK8sVersion, err := version.ParseSemantic(k8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing K8s semantic version")

	const leastSupportedK8sVersion = "v1.30.0"
	parsedLeastSupportedK8sVersion, err := version.ParseSemantic(leastSupportedK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing least supported K8s version")

	latestStableK8sVersion := getLatestStableK8sVersion(ctx)
	parsedLatestStableK8sVersion, err := version.ParseSemantic(latestStableK8sVersion)
	assert.AssertErrNil(ctx, err, "Failed parsing latest stable K8s version")

	// least supported version <= user provided version <= latest stable version.
	//nolint:staticcheck
	if !parsedK8sVersion.AtLeast(parsedLeastSupportedK8sVersion) &&
		!(parsedK8sVersion.LessThan(parsedLatestStableK8sVersion) || parsedK8sVersion.EqualTo(parsedLatestStableK8sVersion)) {

		slog.ErrorContext(ctx, "K8s versions below v1.30.0 aren't supported")
		os.Exit(1)
	}
}

// Fetches and returns the latest stable Kubernetes version, from the Kubeadm API endpoint.
func getLatestStableK8sVersion(ctx context.Context) string {
	const kubeadmAPIURL = "https://dl.k8s.io/release/stable.txt"

	slog.InfoContext(ctx, "Fetching latest stable K8s version", slog.String("URL", kubeadmAPIURL))

	response, err := http.Get(kubeadmAPIURL)
	assert.AssertErrNil(ctx, err, "Failed fetching latest stable K8s version")
	if response.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Failed fetching latest stable Kubernetes version")
		os.Exit(1)
	}
	defer response.Body.Close()

	latestStableK8sVersion, err := io.ReadAll(response.Body)
	assert.AssertErrNil(ctx, err, "Failed reading latest stable K8s version from response body")

	return string(latestStableK8sVersion)
}

// A user defined NodeGroup label key should belong to one of these domains.
// REFER : https://cluster-api.sigs.k8s.io/developer/architecture/controllers/metadata-propagation#machine.
var validNodeGroupLabelDomains = []string{
	"node.cluster.x-k8s.io/",
	"node-role.kubernetes.io/",
	"node-restriction.kubernetes.io/",
}

// Validates node-group labels and taints.
func validateLabelsAndTaints(
	ctx context.Context,
	nodeGroupName string,
	labels map[string]string,
	taints []*coreV1.Taint,
) {
	ctx = logger.AppendSlogAttributesToCtx(ctx, []slog.Attr{
		slog.String("node-group", nodeGroupName),
	})

	// Validate labels.
	//
	// (1) according to Kubernetes specifications.
	err := labelsPkg.Validate(labels)
	assert.AssertErrNil(ctx, err, "MachinePool labels validation failed")
	//
	// (2) according to ClusterAPI specifications.
	for key := range labels {
		// Check if the label belongs to a domain considered valid by ClusterAPI.
		isValidNodeGroupLabelDomain := false
		for _, nodeGroupLabelDomains := range validNodeGroupLabelDomains {
			if strings.HasPrefix(key, nodeGroupLabelDomains) {
				isValidNodeGroupLabelDomain = true
				break
			}
		}
		if !isValidNodeGroupLabelDomain {
			slog.ErrorContext(ctx,
				"NodeGroup label key should belong to one of these domains",
				slog.Any("domains", validNodeGroupLabelDomains),
			)
			os.Exit(1)
		}
	}

	taintsAsKVPairs := map[string]string{}
	for _, taint := range taints {
		taintsAsKVPairs[taint.Key] = fmt.Sprintf("%s:%s", taint.Value, taint.Effect)
	}
	//
	// Validate taints.
	err = labelsPkg.ValidateTaints(taintsAsKVPairs)
	assert.AssertErrNil(ctx, err, "NodeGroup taints validation failed")
}
