package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	chgit "github.com/antham/chyle/chyle/git"
	"github.com/ghodss/yaml"
	"github.com/jenkins-x/jx-helpers/v3/pkg/options"
	"github.com/jenkins-x/jx-helpers/v3/pkg/termcolor"
	"github.com/pkg/errors"
	"github.com/shuttlerock/changlog/pkg/users"
	"github.com/shuttlerock/devops-api/api/v1alpha1"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/jenkins-x-plugins/jx-changelog/pkg/gits"
	"github.com/jenkins-x-plugins/jx-changelog/pkg/helmhelpers"
	"github.com/jenkins-x-plugins/jx-changelog/pkg/issues"
	"github.com/jenkins-x/jx-helpers/v3/pkg/files"
	"github.com/jenkins-x/jx-helpers/v3/pkg/gitclient"
	"github.com/jenkins-x/jx-helpers/v3/pkg/gitclient/cli"
	"github.com/jenkins-x/jx-helpers/v3/pkg/gitclient/giturl"
	"github.com/jenkins-x/jx-helpers/v3/pkg/kube"
	"github.com/jenkins-x/jx-helpers/v3/pkg/scmhelpers"
	"github.com/jenkins-x/jx-helpers/v3/pkg/stringhelpers"

	"github.com/jenkins-x/jx-logging/v3/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ReleaseName = `{{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}`
	SpecName    = `{{ .Chart.Name }}`
)

var (
	info         = termcolor.ColorInfo
	groupCounter = 0
	// ConventionalCommitTitles textual descriptions for
	// Conventional Commit types: https://conventionalcommits.org/
	ConventionalCommitTitles = map[string]*CommitGroup{
		"feat":     createCommitGroup("New Features"),
		"fix":      createCommitGroup("Bug Fixes"),
		"perf":     createCommitGroup("Performance Improvements"),
		"refactor": createCommitGroup("Code Refactoring"),
		"docs":     createCommitGroup("Documentation"),
		"test":     createCommitGroup("Tests"),
		"revert":   createCommitGroup("Reverts"),
		"style":    createCommitGroup("Styles"),
		"chore":    createCommitGroup("Chores"),
		"":         createCommitGroup(""),
	}
	JIRAIssueRegex = regexp.MustCompile(`\b[A-Z][A-Z0-9_]+-\d+\b`)
)

func createCommitGroup(title string) *CommitGroup {
	groupCounter++
	return &CommitGroup{
		Title: title,
		Order: groupCounter,
	}
}

type Options struct {
	options.BaseOptions
	GitDir             string
	OutputMarkdownFile string
	ReleaseYamlFile    string
	ScmFactory         scmhelpers.Options
	State              State
	Version            string
	TemplatesDir       string
	jiraProject        string
	jiraAPIToken       string
	jiraUsername       string
	jiraServerURL      string
}

type State struct {
	Tracker         issues.IssueProvider
	FoundIssueNames map[string]bool
	LoggedIssueKind bool
	Release         *v1alpha1.Release
}

func (o *Options) Validate() error {
	err := o.BaseOptions.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate base options")
	}

	err = o.ScmFactory.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to discover git repository")
	}

	return nil
}

func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate")
	}

	dir := o.ScmFactory.Dir

	previousRev, _, err := gits.GetCommitPointedToByPreviousTag(o.Git(), dir)
	if err != nil {
		return err
	}
	if previousRev == "" {
		// lets assume we are the first release
		previousRev, err = gits.GetFirstCommitSha(o.Git(), dir)
		if err != nil {
			return errors.Wrap(err, "failed to find first commit after we found no previous releaes")
		}
		if previousRev == "" {
			log.Logger().Info("no previous commit version found so change diff unavailable")
			return nil
		}
	}

	currentRev, _, err := gits.GetCommitPointedToByLatestTag(o.Git(), dir)
	if err != nil {
		return err
	}

	templatesDir := o.TemplatesDir
	dir = o.ScmFactory.Dir
	if templatesDir == "" {
		chartFile, err := helmhelpers.FindChart(dir)
		if err != nil {
			return errors.Wrap(err, "could not find helm chart")
		}
		if chartFile == "" {
			log.Logger().Infof("no chart directory found in %s", dir)
			templatesDir = ""
		} else {
			path, _ := filepath.Split(chartFile)
			if path == "" {
				log.Logger().Infof("no chart directory found in %s", dir)
				templatesDir = ""
			} else {
				templatesDir = filepath.Join(path, "templates")
			}
		}
	}
	if templatesDir != "" {
		err = os.MkdirAll(templatesDir, files.DefaultDirWritePermissions)
		if err != nil {
			return errors.Wrapf(err, "failed to create the templates directory %s", templatesDir)
		}
	}

	log.Logger().Infof("Generating change log from git ref %s => %s", info(previousRev), info(currentRev))

	gitDir, gitConfDir, err := gitclient.FindGitConfigDir(dir)
	if err != nil {
		return err
	}
	if gitDir == "" || gitConfDir == "" {
		log.Logger().Warnf("No git directory could be found from dir %s", dir)
		return nil
	}

	gitInfo := o.ScmFactory.GitURL
	if gitInfo == nil {
		gitInfo, err = giturl.ParseGitURL(o.ScmFactory.SourceURL)
		if err != nil {
			return errors.Wrapf(err, "failed to parse git URL %s", o.ScmFactory.SourceURL)
		}
	}

	tracker, err := o.CreateIssueProvider()
	if err != nil {
		return err
	}
	o.State.Tracker = tracker

	o.State.FoundIssueNames = map[string]bool{}

	commits, _ := chgit.FetchCommits(gitDir, previousRev, currentRev)

	if commits != nil {
		commitSlice := *commits
		if len(commitSlice) > 0 {
			if strings.HasPrefix(commitSlice[0].Message, "release ") {
				// remove the release commit from the log
				tmp := commitSlice[1:]
				commits = &tmp
			}
		}
		log.Logger().Debugf("Found commits:")
		if commits != nil {
			for k := range *commits {
				commit := (*commits)[k]
				log.Logger().Debugf("  commit %s", commit.Hash)
				log.Logger().Debugf("  Author: %s <%s>", commit.Author.Name, commit.Author.Email)
				log.Logger().Debugf("  Date: %s", commit.Committer.When.Format(time.ANSIC))
				log.Logger().Debugf("      %s\n\n\n", commit.Message)
			}
		}
	}
	version := o.Version

	release := &v1alpha1.Release{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Release",
			APIVersion: v1alpha1.GroupVersion.Group + "/" + v1alpha1.GroupVersion.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: ReleaseName,
			CreationTimestamp: metav1.Time{
				Time: time.Now(),
			},
			DeletionTimestamp: &metav1.Time{},
		},
		Spec: v1alpha1.ReleaseSpec{
			//Name:          SpecName,
			//Version:       version,
			//GitOwner:      gitInfo.Organisation,
			//GitRepository: gitInfo.Name,
			//GitHTTPURL:    gitInfo.HttpsURL(),
			//GitCloneURL:   gitInfo.CloneURL,
			//Commits:       []v1alpha1.CommitSummary{},
			Issues: []v1alpha1.IssueSummary{},
			//PullRequests:  []v1alpha1.IssueSummary{},
		},
	}

	scmClient := o.ScmFactory.ScmClient
	resolver := users.GitUserResolver{
		GitProvider: scmClient,
	}
	if commits != nil {
		for k := range *commits {
			c := (*commits)[k]
			if len(c.ParentHashes) <= 1 {
				o.addCommit(&release.Spec, &c, &resolver)
			}
		}
	}

	// now lets marshal the release YAML
	data, err := yaml.Marshal(release)

	if err != nil {
		return errors.Wrap(err, "failed to unmarshal Release")
	}
	if data == nil {
		return fmt.Errorf("could not marshal release to yaml")
	}

	if templatesDir != "" {
		releaseFile := filepath.Join(templatesDir, o.ReleaseYamlFile)
		err = ioutil.WriteFile(releaseFile, data, files.DefaultFileWritePermissions)
		if err != nil {
			return errors.Wrapf(err, "failed to save Release YAML file %s", releaseFile)
		}
		log.Logger().Infof("generated: %s", info(releaseFile))
		cleanVersion := strings.TrimPrefix(version, "v")
		release.Spec.Version = cleanVersion
	}

	return nil
}

// CreateIssueProvider creates the issue provider
func (o *Options) CreateIssueProvider() (issues.IssueProvider, error) {
	return issues.CreateJiraIssueProvider(o.jiraServerURL, o.jiraUsername, o.jiraAPIToken, o.jiraProject, true)
}

func (o *Options) Git() gitclient.Interface {
	return cli.NewCLIClient("", nil)
}

func (o *Options) addCommit(spec *v1alpha1.ReleaseSpec, commit *object.Commit, resolver *users.GitUserResolver) {
	// TODO
	url := ""
	branch := "master"

	var author, committer *v1alpha1.UserDetails
	var err error
	sha := commit.Hash.String()
	if commit.Author.Email != "" && commit.Author.Name != "" {
		author, err = resolver.GitSignatureAsUser(&commit.Author)
		if err != nil {
			log.Logger().Warnf("failed to enrich commit with issues, error getting git signature for git author %s: %v", commit.Author, err)
		}
	}
	if commit.Committer.Email != "" && commit.Committer.Name != "" {
		committer, err = resolver.GitSignatureAsUser(&commit.Committer)
		if err != nil {
			log.Logger().Warnf("failed to enrich commit with issues, error getting git signature for git committer %s: %v", commit.Committer, err)
		}
	}
	commitSummary := v1alpha1.CommitSummary{
		Message:   commit.Message,
		URL:       url,
		SHA:       sha,
		Author:    author,
		Branch:    branch,
		Committer: committer,
	}

	o.addIssuesAndPullRequests(spec, &commitSummary, commit)
	spec.Commits = append(spec.Commits, commitSummary)
}

func (o *Options) addIssuesAndPullRequests(spec *v1alpha1.ReleaseSpec, commit *v1alpha1.CommitSummary, rawCommit *object.Commit) {
	tracker := o.State.Tracker

	regex := JIRAIssueRegex
	message := fullCommitMessageText(rawCommit)

	matches := regex.FindAllStringSubmatch(message, -1)

	resolver := users.GitUserResolver{
		GitProvider: o.ScmFactory.ScmClient,
	}
	for _, match := range matches {
		for _, result := range match {
			result = strings.TrimPrefix(result, "#")
			if _, ok := o.State.FoundIssueNames[result]; !ok {
				o.State.FoundIssueNames[result] = true
				issue, err := tracker.GetIssue(result)
				if err != nil {
					log.Logger().Warnf("Failed to lookup issue %s in issue tracker %s due to %s", result, tracker.HomeURL(), err)
					continue
				}
				if issue == nil {
					log.Logger().Warnf("Failed to find issue %s for repository %s", result, tracker.HomeURL())
					continue
				}

				user, err := resolver.Resolve(&issue.Author)
				if err != nil {
					log.Logger().Warnf("Failed to resolve user %v for issue %s repository %s", issue.Author, result, tracker.HomeURL())
				}

				var closedBy *v1alpha1.UserDetails
				if issue.ClosedBy == nil {
					log.Logger().Warnf("Failed to find closedBy user for issue %s repository %s", result, tracker.HomeURL())
				} else {
					u, err := resolver.Resolve(issue.ClosedBy)
					if err != nil {
						log.Logger().Warnf("Failed to resolve closedBy user %v for issue %s repository %s", issue.Author, result, tracker.HomeURL())
					} else if u != nil {
						closedBy = u
					}
				}

				var assignees []v1alpha1.UserDetails
				if issue.Assignees == nil {
					log.Logger().Warnf("Failed to find assignees for issue %s repository %s", result, tracker.HomeURL())
				} else {
					u, err := resolver.GitUserSliceAsUserDetailsSlice(issue.Assignees)
					if err != nil {
						log.Logger().Warnf("Failed to resolve Assignees %v for issue %s repository %s", issue.Assignees, result, tracker.HomeURL())
					}
					assignees = u
				}

				labels := toV1Labels(issue.Labels)
				commit.IssueIDs = append(commit.IssueIDs, result)
				issueSummary := v1alpha1.IssueSummary{
					ID:                result,
					URL:               issue.Link,
					Title:             issue.Title,
					Body:              issue.Body,
					User:              user,
					CreationTimestamp: kube.ToMetaTime(&issue.Created),
					ClosedBy:          closedBy,
					Assignees:         assignees,
					Labels:            labels,
				}
				state := issue.State
				if state != "" {
					issueSummary.State = state
				}
				if issue.PullRequest {
					spec.PullRequests = append(spec.PullRequests, issueSummary)
				} else {
					spec.Issues = append(spec.Issues, issueSummary)
				}
			}
		}
	}
}

// toV1Labels converts git labels to IssueLabel
func toV1Labels(labels []string) []v1alpha1.IssueLabel {
	var answer []v1alpha1.IssueLabel
	for _, label := range labels {
		answer = append(answer, v1alpha1.IssueLabel{
			Name: label,
		})
	}
	return answer
}

// fullCommitMessageText returns the commit message
func fullCommitMessageText(commit *object.Commit) string {
	answer := commit.Message
	fn := func(parent *object.Commit) {
		text := parent.Message
		if text != "" {
			sep := "\n"
			if strings.HasSuffix(answer, "\n") {
				sep = ""
			}
			answer += sep + text
		}
	}
	fn(commit)
	return answer
}

func (o *Options) getTemplateResult(releaseSpec *v1alpha1.ReleaseSpec, templateName, templateText, templateFile string) (string, error) {
	if templateText == "" {
		if templateFile == "" {
			return "", nil
		}
		data, err := ioutil.ReadFile(templateFile)
		if err != nil {
			return "", err
		}
		templateText = string(data)
	}
	if templateText == "" {
		return "", nil
	}
	tmpl, err := template.New(templateName).Parse(templateText)
	if err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	err = tmpl.Execute(writer, releaseSpec)
	writer.Flush()
	return buffer.String(), err
}

func isReleaseNotFound(err error, gitKind string) bool {
	switch gitKind {
	case "gitlab":
		return strings.Contains(err.Error(), "Forbidden") || scmhelpers.IsScmNotFound(err)
	default:
		return scmhelpers.IsScmNotFound(err)
	}
}

type CommitInfo struct {
	Kind    string
	Feature string
	Message string
	group   *CommitGroup
}

type GroupAndCommitInfos struct {
	group   *CommitGroup
	commits []string
}

type CommitGroup struct {
	Title string
	Order int
}

// ParseCommit parses a conventional commit
// see: https://conventionalcommits.org/
func ParseCommit(message string) *CommitInfo {
	answer := &CommitInfo{
		Message: message,
	}

	idx := strings.Index(message, ":")
	if idx > 0 {
		kind := message[0:idx]
		if strings.HasSuffix(kind, ")") {
			ix := strings.Index(kind, "(")
			if ix > 0 {
				answer.Feature = strings.TrimSpace(kind[ix+1 : len(kind)-1])
				kind = strings.TrimSpace(kind[0:ix])
			}
		}
		answer.Kind = kind
		rest := strings.TrimSpace(message[idx+1:])

		answer.Message = rest
	}
	return answer
}

func describeIssueShort(issue *v1alpha1.IssueSummary) string {
	prefix := ""
	id := issue.ID
	if len(id) > 0 {
		// lets only add the hash prefix for numeric ids
		_, err := strconv.Atoi(id)
		if err == nil {
			prefix = "#"
		}
	}
	return "[" + prefix + issue.ID + "](" + issue.URL + ") "
}

func describeUser(info *giturl.GitRepository, user *v1alpha1.UserDetails) string {
	answer := ""
	if user != nil {
		userText := ""
		login := user.Login
		url := user.URL
		label := login
		if label == "" {
			label = user.Name
		}
		if url == "" && login != "" {
			url = stringhelpers.UrlJoin(info.HostURL(), login)
		}
		if url == "" {
			userText = label
		} else if label != "" {
			userText = "[" + label + "](" + url + ")"
		}
		if userText != "" {
			answer = " (" + userText + ")"
		}
	}
	return answer
}

func describeCommit(info *giturl.GitRepository, cs *v1alpha1.CommitSummary, ci *CommitInfo, issueMap map[string]*v1alpha1.IssueSummary) string {
	prefix := ""
	if ci.Feature != "" {
		prefix = ci.Feature + ": "
	}
	message := strings.TrimSpace(ci.Message)
	lines := strings.Split(message, "\n")

	// TODO add link to issue etc...
	user := cs.Author
	if user == nil {
		user = cs.Committer
	}
	issueText := ""
	for k := range cs.IssueIDs {
		issue := issueMap[cs.IssueIDs[k]]
		if issue != nil {
			issueText += " " + describeIssueShort(issue)
		}
	}
	return prefix + lines[0] + describeUser(info, user) + issueText
}

func (c *CommitInfo) Group() *CommitGroup {
	if c.group == nil {
		c.group = ConventionalCommitTitles[strings.ToLower(c.Kind)]
	}
	return c.group
}

func (c *CommitInfo) Title() string {
	return c.Group().Title
}

func (c *CommitInfo) Order() int {
	return c.Group().Order
}
