package cmd

import (
	"context"
	"fmt"

	"github.com/lindell/multi-gitter/internal/http"
	"github.com/lindell/multi-gitter/internal/multigitter"
	"github.com/lindell/multi-gitter/internal/scm/gitea"
	"github.com/lindell/multi-gitter/internal/scm/github"
	"github.com/lindell/multi-gitter/internal/scm/gitlab"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
)

func configurePlatform(cmd *cobra.Command) {
	flags := cmd.Flags()

	flags.StringP("base-url", "g", "", "Base URL of the (v3) GitHub API, needs to be changed if GitHub enterprise is used. Or the url to a self-hosted GitLab instance.")
	flags.StringP("token", "T", "", "The GitHub/GitLab personal access token. Can also be set using the GITHUB_TOKEN/GITLAB_TOKEN environment variable.")

	flags.StringSliceP("org", "O", nil, "The name of a GitHub organization. All repositories in that organization will be used.")
	flags.StringSliceP("group", "G", nil, "The name of a GitLab organization. All repositories in that group will be used.")
	flags.StringSliceP("user", "U", nil, "The name of a user. All repositories owned by that user will be used.")
	flags.StringSliceP("repo", "R", nil, "The name, including owner of a GitHub repository in the format \"ownerName/repoName\".")
	flags.StringSliceP("project", "P", nil, "The name, including owner of a GitLab project in the format \"ownerName/repoName\".")
	flags.BoolP("include-subgroups", "", false, "Include GitLab subgroups when using the --group flag.")

	flags.StringP("platform", "p", "github", "The platform that is used. Available values: github, gitlab, gitea.")
	_ = cmd.RegisterFlagCompletionFunc("platform", func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"github", "gitlab", "gitea"}, cobra.ShellCompDirectiveDefault
	})

	// Autocompletion for organizations
	_ = cmd.RegisterFlagCompletionFunc("org", func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		vc, err := getVersionController(cmd.Flags(), false)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		type getOrger interface {
			GetAutocompleteOrganizations(ctx context.Context, _ string) ([]string, error)
		}

		g, ok := vc.(getOrger)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}

		orgs, err := g.GetAutocompleteOrganizations(cmd.Root().Context(), toComplete)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		return orgs, cobra.ShellCompDirectiveDefault
	})

	// Autocompletion for users
	_ = cmd.RegisterFlagCompletionFunc("user", func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		vc, err := getVersionController(cmd.Flags(), false)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		type getUserser interface {
			GetAutocompleteUsers(ctx context.Context, _ string) ([]string, error)
		}

		g, ok := vc.(getUserser)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}

		users, err := g.GetAutocompleteUsers(cmd.Root().Context(), toComplete)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		return users, cobra.ShellCompDirectiveDefault
	})

	// Autocompletion for repositories
	_ = cmd.RegisterFlagCompletionFunc("repo", func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		vc, err := getVersionController(cmd.Flags(), false)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		type getRepositorieser interface {
			GetAutocompleteRepositories(ctx context.Context, _ string) ([]string, error)
		}

		g, ok := vc.(getRepositorieser)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}

		users, err := g.GetAutocompleteRepositories(cmd.Root().Context(), toComplete)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		return users, cobra.ShellCompDirectiveDefault
	})
}

// OverrideVersionController can be set to force a specific version controller to be used
// This is used to override the version controller with a mock, to be used during testing
var OverrideVersionController multigitter.VersionController = nil

// getVersionController gets the complete version controller
// the verifyFlags parameter can be set to false if a complete vc is not required (during autocompletion)
func getVersionController(flag *flag.FlagSet, verifyFlags bool) (multigitter.VersionController, error) {
	if OverrideVersionController != nil {
		return OverrideVersionController, nil
	}

	platform, _ := flag.GetString("platform")
	switch platform {
	default:
		return nil, fmt.Errorf("unknown platform: %s", platform)
	case "github":
		return createGithubClient(flag, verifyFlags)
	case "gitlab":
		return createGitlabClient(flag, verifyFlags)
	case "gitea":
		return createGiteaClient(flag, verifyFlags)
	}
}

func createGithubClient(flag *flag.FlagSet, verifyFlags bool) (multigitter.VersionController, error) {
	gitBaseURL, _ := flag.GetString("base-url")
	orgs, _ := flag.GetStringSlice("org")
	users, _ := flag.GetStringSlice("user")
	repos, _ := flag.GetStringSlice("repo")
	forkMode, _ := flag.GetBool("fork")

	if verifyFlags && len(orgs) == 0 && len(users) == 0 && len(repos) == 0 {
		return nil, errors.New("no organization, user or repo set")
	}

	token, err := getToken(flag)
	if err != nil {
		return nil, err
	}

	repoRefs := make([]github.RepositoryReference, len(repos))
	for i := range repos {
		repoRefs[i], err = github.ParseRepositoryReference(repos[i])
		if err != nil {
			return nil, err
		}
	}

	mergeTypes, err := getMergeTypes(flag)
	if err != nil {
		return nil, err
	}

	vc, err := github.New(token, gitBaseURL, http.NewLoggingRoundTripper, github.RepositoryListing{
		Organizations: orgs,
		Users:         users,
		Repositories:  repoRefs,
	}, mergeTypes, forkMode)
	if err != nil {
		return nil, err
	}

	return vc, nil
}

func createGitlabClient(flag *flag.FlagSet, verifyFlags bool) (multigitter.VersionController, error) {
	gitBaseURL, _ := flag.GetString("base-url")
	groups, _ := flag.GetStringSlice("group")
	users, _ := flag.GetStringSlice("user")
	projects, _ := flag.GetStringSlice("project")
	includeSubgroups, _ := flag.GetBool("include-subgroups")

	if verifyFlags && len(groups) == 0 && len(users) == 0 && len(projects) == 0 {
		return nil, errors.New("no group user or project set")
	}

	token, err := getToken(flag)
	if err != nil {
		return nil, err
	}

	projRefs := make([]gitlab.ProjectReference, len(projects))
	for i := range projects {
		projRefs[i], err = gitlab.ParseProjectReference(projects[i])
		if err != nil {
			return nil, err
		}
	}

	vc, err := gitlab.New(token, gitBaseURL, gitlab.RepositoryListing{
		Groups:   groups,
		Users:    users,
		Projects: projRefs,
	}, gitlab.Config{
		IncludeSubgroups: includeSubgroups,
	})
	if err != nil {
		return nil, err
	}

	return vc, nil
}

func createGiteaClient(flag *flag.FlagSet, verifyFlags bool) (multigitter.VersionController, error) {
	giteaBaseURL, _ := flag.GetString("base-url")
	orgs, _ := flag.GetStringSlice("org")
	users, _ := flag.GetStringSlice("user")
	repos, _ := flag.GetStringSlice("repo")

	if verifyFlags && len(orgs) == 0 && len(users) == 0 && len(repos) == 0 {
		return nil, errors.New("no organization, user or repository set")
	}

	if giteaBaseURL == "" {
		return nil, errors.New("no base-url set")
	}

	token, err := getToken(flag)
	if err != nil {
		return nil, err
	}

	repoRefs := make([]gitea.RepositoryReference, len(repos))
	for i := range repos {
		repoRefs[i], err = gitea.ParseRepositoryReference(repos[i])
		if err != nil {
			return nil, err
		}
	}

	mergeTypes, err := getMergeTypes(flag)
	if err != nil {
		return nil, err
	}

	vc, err := gitea.New(token, giteaBaseURL, gitea.RepositoryListing{
		Organizations: orgs,
		Users:         users,
		Repositories:  repoRefs,
	}, mergeTypes)
	if err != nil {
		return nil, err
	}

	return vc, nil
}
