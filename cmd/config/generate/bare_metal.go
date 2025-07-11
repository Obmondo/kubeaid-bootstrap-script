package generate

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

var BareMetalCmd = &cobra.Command{
	Use: "bare-metal",

	Short: "Generate a sample KubeAid Bootstrap Script config file, for deploying a Linux bare-metal based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		config.GenerateSampleConfig(context.Background(), &config.GenerateSampleConfigArgs{
			CloudProvider: constants.CloudProviderBareMetal,
		})
	},
}
