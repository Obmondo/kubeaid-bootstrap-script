package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var AWSCmd = &cobra.Command{
	Use: "aws",

	Short: "Bootstrap an AWS based cluster",

	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(),
			managementClusterName,
			skipKubePrometheusBuild,
			skipClusterctlMove,
			false,
		)
	},
}

func init() {
	// Flags.
	config.RegisterAWSCredentialsFlags(AWSCmd)
}
