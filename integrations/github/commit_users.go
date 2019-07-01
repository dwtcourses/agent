package main

import (
	"sync"

	"github.com/pinpt/agent.next/integrations/github/api"
	"github.com/pinpt/agent.next/rpcdef"
)

func (s *Integration) exportCommitUsers(repos []api.Repo, concurrency int) error {
	et, err := s.newExportType("sourcecode.commit_user")
	if err != nil {
		return err
	}
	defer et.Done()

	wg := sync.WaitGroup{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range reposToChan(repos, 0) {
				err := s.exportCommitsForRepoDefaultBranch(et, repo)
				if err != nil {
					panic(err)
				}
			}
		}()
	}
	wg.Wait()
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

func (s *Integration) exportCommitsForRepoDefaultBranch(et *exportType, repo api.Repo) error {
	s.logger.Info("exporting commits (to get users)", "repo_id", repo.ID, "repo_name", repo.Name)

	if repo.DefaultBranch == "" {
		return nil
	}

	err := s.exportCommitsForRepoBranch(et, repo, repo.DefaultBranch)
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

func (s *Integration) exportCommitsForRepoBranch(et *exportType, repo api.Repo, branchName string) error {
	s.logger.Info("exporting commits for branch", "repo_id", repo.ID, "repo_name", repo.Name)

	return api.PaginateCommits(
		et.lastProcessed,
		func(query string) (api.PageInfo, error) {

			pi, res, err := api.CommitsPage(s.qc,
				repo.ID,
				branchName,
				query,
			)
			if err != nil {
				return pi, err
			}

			s.logger.Info("got commits page", "l", len(res))
			batch := []rpcdef.ExportObj{}

			for _, commit := range res {
				author := CommitUser{}
				author.CustomerID = s.customerID
				author.Name = commit.AuthorName
				author.Email = commit.AuthorEmail
				author.SourceID = commit.AuthorRefID
				batch = append(batch, rpcdef.ExportObj{Data: author.ToMap()})

				committer := CommitUser{}
				committer.CustomerID = s.customerID
				committer.Name = commit.CommitterName
				committer.Email = commit.CommitterEmail
				committer.SourceID = commit.CommitterRefID
				batch = append(batch, rpcdef.ExportObj{Data: committer.ToMap()})
			}

			return pi, et.Send(batch)
		})
}

type CommitUser struct {
	CustomerID string
	Email      string
	Name       string
	SourceID   string
}

func (s CommitUser) ToMap() map[string]interface{} {
	res := map[string]interface{}{}
	res["customer_id"] = s.CustomerID
	res["email"] = s.Email
	res["name"] = s.Name
	res["source_id"] = s.SourceID
	return res
}
