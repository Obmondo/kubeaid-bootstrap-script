package devenv

import (
	"github.com/spf13/cobra"

	"github.com/Obmondo/kubeaid-bootstrap-script/cmd/devenv/create"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils"
)

var DevenvCmd = &cobra.Command{
	Use: "devenv",

	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize config.
		config.ParseConfigFiles(cmd.Context(), config.ConfigsDirectory)

		// Initialize temp directory.
		utils.InitTempDir()
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Subcommands.
	DevenvCmd.AddCommand(create.CreateCmd)

	// Flags.
	config.RegisterConfigsDirectoryFlag(DevenvCmd)
}
