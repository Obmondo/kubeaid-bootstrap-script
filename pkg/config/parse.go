package config

import (
	"context"
	"log/slog"
	"os"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/creasty/defaults"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

var (
	GeneralConfigFileContents []byte

	ParsedGeneralConfig = &GeneralConfig{}
	ParsedSecretsConfig = &SecretsConfig{}
)

func ParseConfigFiles(ctx context.Context, configsDirectory string) {
	var err error

	// Read contents of the general config file into ParsedGeneralConfig.
	{
		generalConfigFilePath := path.Join(configsDirectory, constants.FileNameGeneralConfig)

		GeneralConfigFileContents, err = os.ReadFile(generalConfigFilePath)
		assert.AssertErrNil(ctx, err, "Failed reading general config file")

		//nolint:musttag
		err = yaml.Unmarshal([]byte(GeneralConfigFileContents), ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed unmarshalling general config")

		// Set globals.CloudProviderName by detecting the underlying cloud-provider being used.
		detectCloudProviderName()

		// Set defaults.
		err = defaults.Set(ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed setting defaults for parsed general config")

		// If the user has provided a custom CA certificate path,
		// then read and store the custom CA certificate in config.
		hydrateCABundle(ctx)

		// Read SSH key-pairs from provided file paths and store them in config.
		hydrateSSHKeyConfigs()

		// Hydrate with Audit Logging options (if required).
		hydrateWithAuditLoggingOptions()
	}

	// Read contents of the secrets config file into ParsedSecretsConfig.
	// This needs to be done before reading the general config.
	{
		secretsConfigFilePath := path.Join(configsDirectory, constants.FileNameSecretsConfig)

		secretsConfigFileContents, err := os.ReadFile(secretsConfigFilePath)
		assert.AssertErrNil(ctx, err, "Failed reading secrets config file")

		err = yaml.Unmarshal([]byte(secretsConfigFileContents), ParsedSecretsConfig)
		assert.AssertErrNil(ctx, err, "Failed unmarshalling secrets config")

		// The AWS credentials and region were not provided via the config file.
		// We'll retrieve them using the files in ~/.aws.
		// And we panic on failure.

		if (globals.CloudProviderName == constants.CloudProviderAWS) &&
			(ParsedSecretsConfig.AWS == nil) {
			awsCredentials := mustGetCredentialsFromAWSConfigFile(ctx)

			ParsedSecretsConfig.AWS = &AWSCredentials{
				AWSAccessKeyID:     awsCredentials.AccessKeyID,
				AWSSecretAccessKey: awsCredentials.SecretAccessKey,
				AWSSessionToken:    awsCredentials.SessionToken,
			}
		}
	}

	// Set globals.CloudProvider based on the underlying cloud-provider being used.
	setCloudProvider()

	/*
		For each node-group (in the general config), the CPU and memory of the corresponding VM type
		need to specified. This is required by Cluster AutoScaler, for 2 things to work :

		  (1) scale from zero

		  (2) when a node in a node-group is cordoned and there is workload-pressure, the node-group
		      gets scaled up.
	*/
	hydrateVMSpecs(ctx)

	// Validate the general and secrets configs.
	validateConfigs()
}

// Based on the parsed config, detects the underlying cloud-provider name.
// And sets the value of globals.CloudProviderName.
func detectCloudProviderName() {
	switch {
	case ParsedGeneralConfig.Cloud.AWS != nil:
		globals.CloudProviderName = constants.CloudProviderAWS

	case ParsedGeneralConfig.Cloud.Azure != nil:
		globals.CloudProviderName = constants.CloudProviderAzure

	case ParsedGeneralConfig.Cloud.Hetzner != nil:
		globals.CloudProviderName = constants.CloudProviderHetzner

	case ParsedGeneralConfig.Cloud.Local != nil:
		globals.CloudProviderName = constants.CloudProviderLocal

	default:
		slog.Error("No cloud-provider specific details provided")
		os.Exit(1)
	}
}

// Based on the cloud-provider we're using, sets the value of globals.CloudProvider.
func setCloudProvider() {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		globals.CloudProvider = NewAWSCloudProvider()

	case constants.CloudProviderAzure:
		globals.CloudProvider = NewAzureCloudProvider()

	case constants.CloudProviderHetzner:
		globals.CloudProvider = NewHetznerCloudProvider()
	}
}

// Retrieve AWS credentials using the files in ~/.aws.
// Panics on any error.
func mustGetCredentialsFromAWSConfigFile(ctx context.Context) *aws.Credentials {
	slog.InfoContext(ctx, "Trying to pickup AWS credentials from ~/.aws/config")

	awsConfig, err := config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing AWS config using files in ~/.aws")

	awsCredentials, err := awsConfig.Credentials.Retrieve(ctx)
	assert.AssertErrNil(ctx, err, "Failed retrieving AWS credentials from constructed AWS config")

	return &awsCredentials
}

// If the user has provided a custom CA certificate path,
// then reads and stores the custom CA certificate in general config.
func hydrateCABundle(ctx context.Context) {
	caBundlePath := ParsedGeneralConfig.Git.CABundlePath

	if len(caBundlePath) == 0 {
		return
	}

	caBundle, err := os.ReadFile(caBundlePath)
	assert.AssertErrNil(ctx, err, "Failed reading file", slog.String("path", caBundlePath))

	ParsedGeneralConfig.Git.CABundle = caBundle
}

// For each node-group, fills up the cpu and memory (fetched using the corresponding cloud SDK) of
// the corresponding VM type being used.
func hydrateVMSpecs(ctx context.Context) {
	switch globals.CloudProviderName {
	case constants.CloudProviderAWS:
		for i, nodeGroup := range ParsedGeneralConfig.Cloud.AWS.NodeGroups {
			instanceSpecs := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.InstanceType)

			ParsedGeneralConfig.Cloud.AWS.NodeGroups[i].CPU = instanceSpecs.CPU
			ParsedGeneralConfig.Cloud.AWS.NodeGroups[i].Memory = instanceSpecs.Memory
		}

	case constants.CloudProviderAzure:
		for i, nodeGroup := range ParsedGeneralConfig.Cloud.Azure.NodeGroups {
			vmSpecs := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.VMSize)

			ParsedGeneralConfig.Cloud.Azure.NodeGroups[i].CPU = vmSpecs.CPU
			ParsedGeneralConfig.Cloud.Azure.NodeGroups[i].Memory = vmSpecs.Memory
		}

	case constants.CloudProviderHetzner:
		for i, nodeGroup := range ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud {
			machineSpecs := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.MachineType)
			assert.AssertNotNil(
				ctx,
				machineSpecs.RootVolumeSize,
				"Implementation error : machine details returned by HetznerCloudProvider.GetVMSpecs() must include RootVolumeSize",
			)

			ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud[i].CPU = machineSpecs.CPU
			ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud[i].Memory = machineSpecs.Memory
			ParsedGeneralConfig.Cloud.Hetzner.NodeGroups.HCloud[i].RootVolumeSize = *machineSpecs.RootVolumeSize
		}

	case constants.CloudProviderLocal:
		return

	default:
		panic("unreachable")
	}
}
