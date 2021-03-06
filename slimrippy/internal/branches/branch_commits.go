package branches

import (
	"fmt"
	"sort"

	"github.com/pinpt/agent/slimrippy/internal/parentsgraph"
)

// reachableFromHead is a map that has true for all commits that belong to head
// (in default ripsrc config, that will be the default branch)
// map[commitSha]isReachableFromHead
type reachableFromHead map[string]bool

func newReachableFromHead(gr *parentsgraph.Graph, defaultHead string) reachableFromHead {
	res := map[string]bool{}
	done := map[string]bool{}
	var rec func(string)
	rec = func(hash string) {
		if done[hash] {
			return
		}
		done[hash] = true
		res[hash] = true
		for _, p := range gr.Parents[hash] {
			rec(p)
		}
	}
	rec(defaultHead)
	return res
}

func branchCommits(
	gr *parentsgraph.Graph,
	defaultHead string,
	reachableFromHead reachableFromHead,
	branchHead string) (commits []string, branchedFrom []string, rerr error) {

	if reachableFromHead[branchHead] {
		// this is a merged commit, we would need to recreate reachableFromHead without merge commit
		// this is an expensive operation
		reachableFromHead = map[string]bool{}
		done := map[string]bool{}
		var rec func(string)
		rec = func(hash string) {
			if done[hash] {
				return
			}
			done[hash] = true

			reachableFromHead[hash] = true
			if hash == branchHead {
				// remove merge commit to branch head
				return
			}
			par, ok := gr.Parents[hash]
			if !ok {
				rerr = fmt.Errorf("commit not found in tree: %v", hash)
				return
			}
			for _, p := range par {
				rec(p)
			}
		}
		rec(defaultHead)
	}

	commitsDone := map[string]bool{}

	var rec func(string)
	rec = func(hash string) {
		if commitsDone[hash] {
			return
		}
		commits = append(commits, hash)
		commitsDone[hash] = true

		par, ok := gr.Parents[hash]
		if !ok {
			rerr = fmt.Errorf("commit not found in tree: %v", hash)
			return
		}
		// reverse order for better result ordering (see tests)
		for i := len(par) - 1; i >= 0; i-- {
			p := par[i]
			if reachableFromHead[p] {
				branchedFrom = append(branchedFrom, p)
				continue
			}
			rec(p)
		}
	}
	rec(branchHead)
	reverseStrings(commits)

	branchedFrom = dedupLinearFromHead(gr, branchedFrom, branchHead)
	return
}

func dedupLinearFromHead(gr *parentsgraph.Graph, commits []string, defaultHead string) []string {
	hasDeep := map[string]bool{}
	commitsHash := toSet(commits)
	{
		var rec func(string) bool
		done := map[string]bool{}
		rec = func(hash string) (has bool) {
			if done[hash] {
				return hasDeep[hash]
			}
			done[hash] = true
			if commitsHash[hash] {
				has = true
			}
			for _, p := range gr.Parents[hash] {
				r := rec(p)
				if r {
					has = true
				}
			}
			hasDeep[hash] = has
			return
		}
		rec(defaultHead)
	}

	dup := map[string]bool{}
	{
		for h := range commitsHash {
			d := false
			for _, p := range gr.Parents[h] {
				if hasDeep[p] {
					d = true
				}
			}
			dup[h] = d
		}
	}

	var res []string
	for c := range commitsHash {
		if !dup[c] {
			res = append(res, c)
		}
	}
	sort.Strings(res) // to have consistent order
	return res
}

func toSet(arr []string) map[string]bool {
	res := map[string]bool{}
	for _, v := range arr {
		res[v] = true
	}
	return res
}

func reverseStrings(arr []string) {
	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}
}
