package cmd

import (
	"github.com/spf13/cobra"

	command "github.com/shuttlerock/changlog/pkg/cmd"
)

const (
	TemplatesDirFlag = "templates-dir"
	ReleaseYamlFlag  = "release-yaml-file"
	GitDirFlag       = "dir"
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
	createCmd, options := NewCmdChangelogCreate()
	rootCmd.AddCommand(createCmd)
	createCmd.Flags()
	createCmd.Flags().StringVarP(&options.TemplatesDir, TemplatesDirFlag, "t", "", "the directory containing the helm chart templates to generate the resources")
	createCmd.Flags().StringVarP(&options.ReleaseYamlFile, ReleaseYamlFlag, "", "release.yaml", "the name of the file to generate the Release YAML")
	createCmd.Flags().StringVarP(&options.GitDir, GitDirFlag, "", ".", "the directory to search for the .git to discover the git source URL")
	createCmd.Flags().StringVarP(&options.OutputMarkdownFile, "output-markdown", "", "", "Put the changelog output in this file")
}
