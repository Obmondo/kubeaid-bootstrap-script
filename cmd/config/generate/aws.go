package generate

import (
	"context"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying an AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(context.Background(), constants.CloudProviderAWS)
	},
}
