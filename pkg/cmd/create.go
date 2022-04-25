package cmd

import (
	"fmt"
	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"github.com/shuttlerock/changlog/pkg/files"
	"github.com/shuttlerock/devops-api/api/v1alpha1"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"path/filepath"
)

type Options struct {
	ReleaseYamlFile string
	TemplatesDir    *string
}

func (o *Options) Validate() error {
	if o.TemplatesDir == nil {
		return fmt.Errorf("template directory must be specified")
	}
	return nil
}

func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrap(err, "options failed validation")
	}
	release := &v1alpha1.Release{
		Spec: v1alpha1.ReleaseSpec{
			Issues: nil,
		},
	}
	data, err := yaml.Marshal(release)
	if err != nil {
		return errors.Wrap(err, "failed to marshal Release")
	}
	if data == nil {
		return fmt.Errorf("could not marshal release to yaml")
	}

	releaseFile := filepath.Join(*o.TemplatesDir, o.ReleaseYamlFile)
	err = ioutil.WriteFile(releaseFile, data, files.DefaultFileWritePermissions)
	if err != nil {
		return errors.Wrapf(err, "failed to save Release YAML file %s", releaseFile)
	}
	log.Infof("generated: %s", releaseFile)

	return nil
}
