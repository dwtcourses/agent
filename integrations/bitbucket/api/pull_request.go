package api

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/pinpt/agent/integrations/pkg/commonrepo"
	"github.com/pinpt/agent/integrations/pkg/objsender"

	"github.com/pinpt/agent/pkg/date"
	"github.com/pinpt/agent/pkg/ids"
	"github.com/pinpt/go-common/hash"
	pstrings "github.com/pinpt/go-common/strings"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func PullRequestPage(
	qc QueryContext,
	reviewsSender *objsender.Session,
	repo commonrepo.Repo,
	params url.Values,
	stopOnUpdatedAt time.Time) (pi PageInfo, res []sourcecode.PullRequest, err error) {

	if !stopOnUpdatedAt.IsZero() {
		params.Set("q", fmt.Sprintf(" updated_on > %s", stopOnUpdatedAt.UTC().Format("2006-01-02T15:04:05.000000-07:00")))
	}

	objectPath := pstrings.JoinURL("repositories", repo.NameWithOwner, "pullrequests")
	params.Add("state", "MERGED")
	params.Add("state", "SUPERSEDED")
	params.Add("state", "OPEN")
	params.Add("state", "DECLINED")
	params.Set("sort", "-updated_on")
	// Greater than 50 throws "Invalid pagelen"
	params.Set("pagelen", "50")

	qc.Logger.Debug("repo pull requests", "repo", repo.RefID, "repo_name", repo.NameWithOwner, "params", params)

	var rprs []struct {
		RefID  int64 `json:"id"`
		Source struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
		} `json:"source"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Links       struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
		CreatedOn time.Time `json:"created_on"`
		UpdatedOn time.Time `json:"updated_on"`
		State     string    `json:"state"`
		ClosedBy  struct {
			AccountID string `json:"account_id"`
		} `json:"closed_by"`
		MergeCommit struct {
			Hash string `json:"hash"`
		} `json:"merge_commit"`
		Author struct {
			AccountID string `json:"account_id"`
		} `json:"author"`
		Participants []struct {
			Role           string    `json:"role"`
			Approved       bool      `json:"approved"`
			ParticipatedOn time.Time `json:"participated_on"`
			User           struct {
				AccountID string `json:"account_id"`
			} `json:"user"`
		} `json:"participants"`
	}

	pi, err = qc.Request(objectPath, params, true, &rprs)
	if err != nil {
		return
	}

	for _, rpr := range rprs {
		pr := sourcecode.PullRequest{}
		pr.CustomerID = qc.CustomerID
		pr.RefType = qc.RefType
		pr.RefID = strconv.FormatInt(rpr.RefID, 10)
		pr.RepoID = qc.IDs.CodeRepo(repo.RefID)
		pr.BranchName = rpr.Source.Branch.Name
		pr.Title = rpr.Title
		pr.Description = rpr.Description
		pr.URL = rpr.Links.HTML.Href
		pr.Identifier = fmt.Sprintf("#%d", rpr.RefID) // in bitbucket looks like #1 is the format for PR identifiers in their UI
		date.ConvertToModel(rpr.CreatedOn, &pr.CreatedDate)
		date.ConvertToModel(rpr.UpdatedOn, &pr.MergedDate)
		date.ConvertToModel(rpr.UpdatedOn, &pr.ClosedDate)
		date.ConvertToModel(rpr.UpdatedOn, &pr.UpdatedDate)
		switch rpr.State {
		case "OPEN":
			pr.Status = sourcecode.PullRequestStatusOpen
		case "DECLINED":
			pr.Status = sourcecode.PullRequestStatusClosed
			pr.ClosedByRefID = rpr.ClosedBy.AccountID
		case "MERGED":
			pr.MergeSha = rpr.MergeCommit.Hash
			pr.MergeCommitID = ids.CodeCommit(qc.CustomerID, qc.RefType, pr.RepoID, rpr.MergeCommit.Hash)
			pr.MergedByRefID = rpr.ClosedBy.AccountID
			pr.Status = sourcecode.PullRequestStatusMerged
		default:
			qc.Logger.Error("PR has an unknown state", "state", rpr.State, "ref_id", pr.RefID)
		}
		pr.CreatedByRefID = rpr.Author.AccountID

		res = append(res, pr)

		for _, participant := range rpr.Participants {

			review := &sourcecode.PullRequestReview{}

			review.CustomerID = qc.CustomerID
			review.RefType = qc.RefType
			review.RefID = hash.Values(pr.RefID, participant.User.AccountID)
			review.RepoID = qc.IDs.CodeRepo(repo.RefID)
			review.PullRequestID = qc.IDs.CodePullRequest(review.RepoID, strconv.FormatInt(rpr.RefID, 10))

			if participant.Approved {
				review.State = sourcecode.PullRequestReviewStateApproved
			} else if participant.Role == "PARTICIPANT" {
				review.State = sourcecode.PullRequestReviewStateCommented
			} else if participant.Role == "REVIEWER" {
				review.State = sourcecode.PullRequestReviewStatePending
			}

			review.UserRefID = participant.User.AccountID

			date.ConvertToModel(participant.ParticipatedOn, &review.CreatedDate)

			if err = reviewsSender.SetTotal(1); err != nil {
				return pi, res, err
			}
			if err = reviewsSender.Send(review); err != nil {
				return pi, res, err
			}
		}

	}

	return
}
