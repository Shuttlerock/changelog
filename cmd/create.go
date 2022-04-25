package cmd

import (
	"github.com/spf13/cobra"

	command "github.com/shuttlerock/changlog/pkg/cmd"
)

func NewCmdChangelogCreate() (*cobra.Command, *command.Options) {
	o := &command.Options{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Creates a changelog for the release",
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			handleError(err)
		},
	}
	return cmd, o
}

func init() {
	createCmd, _ := NewCmdChangelogCreate()
	rootCmd.AddCommand(createCmd)
	createCmd.Flags()
}
