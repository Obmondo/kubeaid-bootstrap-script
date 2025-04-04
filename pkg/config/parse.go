package config

import (
	"context"
	"log/slog"
	"os"
	"path"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud/hetzner"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/globals"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/creasty/defaults"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

var (
	ParsedGeneralConfig = &GeneralConfig{}
	ParsedSecretsConfig = &SecretsConfig{}
)

func ParseConfigFiles(ctx context.Context, configsDirectory string) {
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
		// And we panic if any error occurs.

		usingAWS := (ParsedSecretsConfig.Azure == nil) && (ParsedSecretsConfig.Hetzner == nil)

		if usingAWS && (ParsedSecretsConfig.AWS == nil) {
			awsCredentials := mustGetCredentialsFromAWSConfigFile(ctx)

			slog.InfoContext(ctx, "Using AWS credentials from ~/.aws/config")
			ParsedSecretsConfig.AWS = &AWSCredentials{
				AWSAccessKeyID:     awsCredentials.AccessKeyID,
				AWSSecretAccessKey: awsCredentials.SecretAccessKey,
				AWSSessionToken:    awsCredentials.SessionToken,
			}
		}
	}

	// Read contents of the general config file into ParsedGeneralConfig.
	{
		generalConfigFilePath := path.Join(configsDirectory, constants.FileNameGeneralConfig)

		generalConfigFileContents, err := os.ReadFile(generalConfigFilePath)
		assert.AssertErrNil(ctx, err, "Failed reading general config file")

		err = yaml.Unmarshal([]byte(generalConfigFileContents), ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed unmarshalling general config")

		// Set globals.CloudProviderName and globals.CloudProvider by detecting the underlying
		// cloud-platform being used.
		detectCloudProvider()

		// Set defaults.
		err = defaults.Set(ParsedGeneralConfig)
		assert.AssertErrNil(ctx, err, "Failed setting defaults for parsed general config")

		// Read SSH key-pairs from provided file paths and store them in config.
		hydrateSSHKeyConfigs()

		// Hydrate with Audit Logging options (if required).
		hydrateWithAuditLoggingOptions()

		/*
			For each node-group, the CPU and memory of the corresponding VM type need to specified.
			This is required by Cluster AutoScaler, for 2 things to work :

			(1) scale from zero

			(2) when a node in a node-group is cordoned and there is workload-pressure, the node-group
					gets scaled up.
		*/
		// NOTE : Always make sure this gets called after readCloudCredentialsFromFlagsToConfig(),
		//        since the cloud credentials from the parsed config are required to construct the
		//        cloud client.
		hydrateVMSpecs(ctx)
	}

	// Validate.
	validateConfig()
}

// Based on the parsed config, detects the underlying cloud-provider name.
// It then sets the value of globals.CloudProviderName and globals.CloudProvider.
func detectCloudProvider() {
	switch {
	case ParsedGeneralConfig.Cloud.AWS != nil:
		globals.CloudProviderName = constants.CloudProviderAWS
		globals.CloudProvider = NewAWSCloudProvider()

	case ParsedGeneralConfig.Cloud.Azure != nil:
		globals.CloudProviderName = constants.CloudProviderAzure
		globals.CloudProvider = NewAzureCloudProvider()

	case ParsedGeneralConfig.Cloud.Hetzner != nil:
		globals.CloudProviderName = constants.CloudProviderHetzner
		globals.CloudProvider = hetzner.NewHetznerCloudProvider()

	case ParsedGeneralConfig.Cloud.Local != nil:
		globals.CloudProviderName = constants.CloudProviderLocal

	default:
		slog.Error("No cloud specific details provided")
		os.Exit(1)
	}
}

// Retrieve AWS credentials using the files in ~/.aws.
// Panics on any error.
func mustGetCredentialsFromAWSConfigFile(ctx context.Context) *aws.Credentials {
	slog.InfoContext(ctx, "Detected ~/.aws")

	awsConfig, err := config.LoadDefaultConfig(ctx)
	assert.AssertErrNil(ctx, err, "Failed constructing AWS config using files in ~/.aws")

	awsCredentials, err := awsConfig.Credentials.Retrieve(ctx)
	assert.AssertErrNil(ctx, err, "Failed retrieving AWS credentials from constructed AWS config")

	return &awsCredentials
}

func hydrateSSHKeyConfigs() {
	switch globals.CloudProviderName {
	case constants.CloudProviderHetzner:
		// When using Hetzner Bare Metal.
		if (ParsedGeneralConfig.Cloud.Hetzner.HetznerBareMetal != nil) && ParsedGeneralConfig.Cloud.Hetzner.HetznerBareMetal.Enabled {
			hydrateSSHKeyConfig(&ParsedGeneralConfig.Cloud.Hetzner.HetznerBareMetal.RobotSSHKeyPair)
		}
	}
}

// Reads and validates an SSH key-pair from the provided file paths.
// The key-pair is then stored in the SSH key config struct itself.
func hydrateSSHKeyConfig(sshKeyConfig *SSHKeyPairConfig) {
	ctx := context.Background()

	// Read and validate the SSH public key.

	publicKey, err := os.ReadFile(sshKeyConfig.PublicKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PublicKeyFilePath),
	)

	_, _, _, _, err = ssh.ParseAuthorizedKey(publicKey)
	assert.AssertErrNil(ctx, err,
		"SSH public key is invalid : failed parsing",
		slog.String("path", sshKeyConfig.PublicKeyFilePath),
	)

	sshKeyConfig.PublicKey = string(publicKey)

	// Read and validate the SSH private key.

	privateKey, err := os.ReadFile(sshKeyConfig.PrivateKeyFilePath)
	assert.AssertErrNil(ctx, err,
		"Failed reading file",
		slog.String("path", sshKeyConfig.PrivateKeyFilePath),
	)

	_, err = ssh.ParsePrivateKey(privateKey)
	assert.AssertErrNil(ctx, err,
		"SSH private key is invalid : failed parsing",
		slog.String("path", sshKeyConfig.PrivateKeyFilePath),
	)

	sshKeyConfig.PrivateKey = string(privateKey)
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
			instanceSpecs := globals.CloudProvider.GetVMSpecs(ctx, nodeGroup.VMSize)

			ParsedGeneralConfig.Cloud.Azure.NodeGroups[i].CPU = instanceSpecs.CPU
			ParsedGeneralConfig.Cloud.Azure.NodeGroups[i].Memory = instanceSpecs.Memory
		}

	case constants.CloudProviderHetzner:
		panic("unimplemented")

	case constants.CloudProviderLocal:
		return

	default:
		panic("unreachable")
	}
}
