package bootstrap

import (
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/core"
	"github.com/spf13/cobra"
)

var HetznerCmd = &cobra.Command{
	Use:   "hetzner",
	Short: "Bootstrap a self-managed Kubernetes cluster in Hetzner (bare-metal)",
	Run: func(cmd *cobra.Command, args []string) {
		core.BootstrapCluster(cmd.Context(), skipKubePrometheusBuild, skipClusterctlMove, false)
	},
}

func init() {
	// Flags.
	config.RegisterHetznerCredentialsFlags(HetznerCmd)
}
