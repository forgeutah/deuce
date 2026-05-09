package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/go-github/v72/github"
)

type orgResult struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

type repoResult struct {
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	CloneURL      string `json:"cloneUrl"`
	Description   string `json:"description"`
	Language      string `json:"language"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
}

var (
	repoCache    = make(map[string]cachedRepos)
	repoCacheMu  sync.Mutex
	repoCacheTTL = 5 * time.Minute

	orgCache     []orgResult
	orgCacheTime time.Time
)

type cachedRepos struct {
	repos []repoResult
	time  time.Time
}

func (h *Handler) ensureGitHub(w http.ResponseWriter) bool {
	if h.githubToken == "" {
		writeError(w, http.StatusServiceUnavailable, "NO_GITHUB_TOKEN", "GitHub token not configured. Set GITHUB_TOKEN env var")
		return false
	}
	return true
}

// ListGitHubOrgs discovers orgs by fetching ALL repos the user can access
// and extracting unique owners. No org scope needed — just repo access.
func (h *Handler) ListGitHubOrgs(w http.ResponseWriter, r *http.Request) {
	if !h.ensureGitHub(w) {
		return
	}

	// Check cache
	repoCacheMu.Lock()
	if orgCache != nil && time.Since(orgCacheTime) < repoCacheTTL {
		cached := orgCache
		repoCacheMu.Unlock()
		writeJSON(w, http.StatusOK, cached)
		return
	}
	repoCacheMu.Unlock()

	client := github.NewClient(nil).WithAuthToken(h.githubToken)

	// Fetch ALL repos the user has access to (personal + org + collaborator)
	opt := &github.RepositoryListByAuthenticatedUserOptions{
		Visibility:  "all",
		Affiliation: "owner,organization_member,collaborator",
		Sort:        "updated",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	// Track unique owners
	seen := make(map[string]orgResult)

	for {
		repos, resp, err := client.Repositories.ListByAuthenticatedUser(r.Context(), opt)
		if err != nil {
			writeError(w, http.StatusBadGateway, "GITHUB_ERROR", "Failed to fetch repos: "+err.Error())
			return
		}
		for _, repo := range repos {
			owner := repo.GetOwner()
			if owner == nil {
				continue
			}
			login := owner.GetLogin()
			if _, exists := seen[login]; !exists {
				seen[login] = orgResult{
					Login:     login,
					AvatarURL: owner.GetAvatarURL(),
				}
			}
		}

		// Also cache repos by owner while we have them
		for _, repo := range repos {
			owner := repo.GetOwner()
			if owner == nil {
				continue
			}
			ownerLogin := owner.GetLogin()
			repoCacheMu.Lock()
			cached, ok := repoCache[ownerLogin]
			if !ok {
				cached = cachedRepos{time: time.Now()}
			}
			cached.repos = append(cached.repos, repoResult{
				Name:          repo.GetName(),
				FullName:      repo.GetFullName(),
				CloneURL:      repo.GetCloneURL(),
				Description:   repo.GetDescription(),
				Language:      repo.GetLanguage(),
				Private:       repo.GetPrivate(),
				DefaultBranch: repo.GetDefaultBranch(),
			})
			cached.time = time.Now()
			repoCache[ownerLogin] = cached
			repoCacheMu.Unlock()
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	result := make([]orgResult, 0, len(seen))
	for _, org := range seen {
		result = append(result, org)
	}

	// Cache orgs
	repoCacheMu.Lock()
	orgCache = result
	orgCacheTime = time.Now()
	repoCacheMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}

// ListGitHubRepos returns repos for a given owner (user or org).
// Query param: ?owner=kollalabs (required)
func (h *Handler) ListGitHubRepos(w http.ResponseWriter, r *http.Request) {
	if !h.ensureGitHub(w) {
		return
	}

	owner := r.URL.Query().Get("owner")
	if owner == "" {
		writeError(w, http.StatusBadRequest, "MISSING_OWNER", "owner query parameter is required")
		return
	}

	// Check cache (may have been populated by ListGitHubOrgs)
	repoCacheMu.Lock()
	if cached, ok := repoCache[owner]; ok && time.Since(cached.time) < repoCacheTTL {
		repoCacheMu.Unlock()
		writeJSON(w, http.StatusOK, cached.repos)
		return
	}
	repoCacheMu.Unlock()

	client := github.NewClient(nil).WithAuthToken(h.githubToken)

	opt := &github.RepositoryListByUserOptions{
		Sort:        "updated",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allRepos []*github.Repository
	for {
		repos, resp, err := client.Repositories.ListByUser(r.Context(), owner, opt)
		if err != nil {
			writeError(w, http.StatusBadGateway, "GITHUB_ERROR", "Failed to fetch repos: "+err.Error())
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

	repoCacheMu.Lock()
	repoCache[owner] = cachedRepos{repos: result, time: time.Now()}
	repoCacheMu.Unlock()

	writeJSON(w, http.StatusOK, result)
}
