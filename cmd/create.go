package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	command "github.com/shuttlerock/changlog/pkg/cmd"
)

const (
	TemplatesDirFlag = "templates-dir"
)

type Options struct {
	ReleaseYamlFile string
	TemplatesDir    string
}

func (o *Options) toCommandOptions(cmd *cobra.Command) (*command.Options, error) {
	var templatesDir *string
	if cmd.Flags().Changed(TemplatesDirFlag) {
		tDir, err := cmd.Flags().GetString(TemplatesDirFlag)
		templatesDir = &tDir
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read %s", TemplatesDirFlag)
		}
	}
	return &command.Options{
		ReleaseYamlFile: "",
		TemplatesDir:    templatesDir,
	}, nil
}

func NewCmdChangelogCreate() (*cobra.Command, *Options) {
	o := &Options{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Creates a changelog for the release",
		Run: func(cmd *cobra.Command, args []string) {
			commandOptions, err := o.toCommandOptions(cmd)
			if err != nil {
				handleError(err)
			}
			err = commandOptions.Run()
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
	createCmd.Flags().StringVarP(&options.ReleaseYamlFile, "release-yaml-file", "", "release.yaml", "the name of the file to generate the Release YAML")
}
