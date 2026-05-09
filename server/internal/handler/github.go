package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/go-github/v72/github"
)

type repoResult struct {
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	CloneURL      string `json:"cloneUrl"`
	Description   string `json:"description"`
	Language      string `json:"language"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

// Simple in-memory cache for GitHub repos
var (
	repoCache     []repoResult
	repoCacheTime time.Time
	repoCacheMu   sync.Mutex
	repoCacheTTL  = 5 * time.Minute
)

func (h *Handler) ListGitHubRepos(w http.ResponseWriter, r *http.Request) {
	if h.githubToken == "" {
		writeError(w, http.StatusServiceUnavailable, "NO_GITHUB_TOKEN", "GitHub token not configured. Set GITHUB_TOKEN env var")
		return
	}

	// Check cache
	repoCacheMu.Lock()
	if repoCache != nil && time.Since(repoCacheTime) < repoCacheTTL {
		cached := repoCache
		repoCacheMu.Unlock()
		writeJSON(w, http.StatusOK, cached)
		return
	}
	repoCacheMu.Unlock()

	client := github.NewClient(nil).WithAuthToken(h.githubToken)

	opt := &github.RepositoryListByAuthenticatedUserOptions{
		Visibility:  "all",
		Affiliation: "owner",
		Sort:        "updated",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allRepos []*github.Repository
	for {
		repos, resp, err := client.Repositories.ListByAuthenticatedUser(r.Context(), opt)
		if err != nil {
			writeError(w, http.StatusBadGateway, "GITHUB_ERROR", "Failed to fetch repos from GitHub: "+err.Error())
			return
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	result := make([]repoResult, 0, len(allRepos))
	for _, r := range allRepos {
		result = append(result, repoResult{
			Name:          r.GetName(),
			FullName:      r.GetFullName(),
			CloneURL:      r.GetCloneURL(),
			Description:   r.GetDescription(),
			Language:      r.GetLanguage(),
			Private:       r.GetPrivate(),
			DefaultBranch: r.GetDefaultBranch(),
		})
	}

	// Update cache
	repoCacheMu.Lock()
	repoCache = result
	repoCacheTime = time.Now()
	repoCacheMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}
