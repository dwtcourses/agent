package main

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent/pkg/commitusers"

	"github.com/pinpt/agent/integrations/github/api"
	"github.com/pinpt/agent/integrations/pkg/objsender"
	"github.com/pinpt/agent/integrations/pkg/repoprojects"
)

func (s *Integration) exportCommitUsersForRepo(ctx *repoprojects.ProjectCtx, repo api.RepoWithDefaultBranch) error {
	logger := ctx.Logger
	usersSender, err := ctx.Session(commitusers.TableName)
	if err != nil {
		return err
	}
	err = s.exportCommitsForRepoDefaultBranch(logger, usersSender, repo)
	if err != nil {
		return err
	}
	return nil
}

// maxToReturn useful for debugging
func reposToChan(sl []api.Repo, maxToReturn int) chan api.Repo {
	res := make(chan api.Repo)
	go func() {
		defer close(res)
		for i, a := range sl {
			if maxToReturn != 0 {
				if i == maxToReturn {
					return
				}
			}
			res <- a
		}
	}()
	return res
}

func (s *Integration) exportCommitsForRepoDefaultBranch(logger hclog.Logger, userSender *objsender.Session, repo api.RepoWithDefaultBranch) error {
	logger.Info("exporting commits (to get users)", "repo_id", repo.ID, "repo_name", repo.NameWithOwner)

	if repo.DefaultBranch == "" {
		return nil
	}

	err := s.exportCommitsForRepoBranch(logger, userSender, repo, repo.DefaultBranch)
	if err != nil {
		return err
	}

	return nil
}

/*
// unused right now, only getting commits for default branch
func (s *Integration) exportCommitsForRepoAllBranches(et *exportType, repoID string) error {
	s.logger.Info("exporting commits (to get users)", "repo", repoID)
	branches := make(chan []string)
	go func() {
		defer close(branches)
		err := api.BranchNames(s.qc, repoID, branches)
		if err != nil {
			panic(err)
		}
	}()
	for sl := range branches {
		for _, branch := range sl {
			err := s.exportCommitsForRepoBranch(et, repoID, branch)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
*/

func (s *Integration) exportCommitsForRepoBranch(logger hclog.Logger, userSender *objsender.Session, repo api.RepoWithDefaultBranch, branchName string) error {
	logger.Info("exporting commits for branch", "repo_id", repo.ID, "repo_name", repo.NameWithOwner)

	return api.PaginateCommits(
		userSender.LastProcessedTime(),
		func(query string) (api.PageInfo, error) {

			pi, res, err := api.CommitsPage(s.qc.WithLogger(logger),
				repo.Repo(),
				branchName,
				query,
			)
			if err != nil {
				return pi, err
			}

			logger.Info("got commits page", "l", len(res))

			for _, commit := range res {
				validate := func(u commitusers.CommitUser, kind string) error {
					err := u.Validate()
					if err != nil {
						return fmt.Errorf("commit data does not have proper %v repo: %v commit: %v %v", kind, repo.NameWithOwner, commit.CommitHash, err)
					}
					return nil
				}

				author := commitusers.CommitUser{}
				author.CustomerID = s.customerID
				author.Name = commit.AuthorName
				author.Email = commit.AuthorEmail
				author.SourceID = commit.AuthorRefID

				committer := commitusers.CommitUser{}
				committer.CustomerID = s.customerID
				committer.Name = commit.CommitterName
				committer.Email = commit.CommitterEmail
				committer.SourceID = commit.CommitterRefID

				err := validate(author, "author")
				if err != nil {
					// TODO: some commits don't have associated emails, but that is not an error we are logging it here to validate in more details in the future
					// if it's all ok can remove the warning as well
					s.logger.Warn("commit user", "err", err)
				} else {
					err := userSender.SendMap(author.ToMap())
					if err != nil {
						return pi, err
					}
				}

				err = validate(committer, "commiter")
				if err != nil {
					s.logger.Warn("commit user", "err", err)
				} else {
					err := userSender.SendMap(committer.ToMap())
					if err != nil {
						return pi, err
					}
				}
			}

			return pi, nil
		})
}
