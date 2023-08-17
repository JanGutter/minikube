/*
Copyright 2020 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package update

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/google/go-github/v54/github"
)

const (
	// ghListPerPage uses max value (100) for PerPage to avoid hitting the rate limits.
	// (ref: https://pkg.go.dev/github.com/google/go-github/github#hdr-Rate_Limiting)
	ghListPerPage = 100

	// ghSearchLimit limits the number of searched items to be <= N * ghListPerPage.
	ghSearchLimit = 300
)

type Release struct {
	Tag    string
	Commit string
}

// GHReleases returns greatest current stable release and greatest latest rc or beta pre-release from GitHub owner/repo repository, and any error occurred.
// If latest pre-release version is lower than the current stable release, then it will return current stable release for both.
func GHReleases(ctx context.Context, owner, repo string) (stable, latest, edge Release, err error) {
	ghc := github.NewClient(nil)

	// walk through the paginated list of up to ghSearchLimit newest releases
	opts := &github.ListOptions{PerPage: ghListPerPage}
	for (opts.Page+1)*ghListPerPage <= ghSearchLimit {
		rls, resp, err := ghc.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return stable, latest, edge, err
		}
		for _, rl := range rls {
			ver := rl.GetTagName()
			if !semver.IsValid(ver) {
				continue
			}
			// check if ver version is release (ie, 'v1.19.2') or pre-release (ie, 'v1.19.3-rc.0' or 'v1.19.0-beta.2')
			prerls := semver.Prerelease(ver)
			if prerls == "" {
				if semver.Compare(ver, stable.Tag) == 1 {
					stable.Tag = ver
				}
			} else if strings.HasPrefix(prerls, "-rc") || strings.HasPrefix(prerls, "-beta") {
				if semver.Compare(ver, latest.Tag) == 1 {
					latest.Tag = ver
				}
			} else if strings.Contains(prerls, "-alpha") {
				if semver.Compare(ver, edge.Tag) == 1 {
					edge.Tag = ver
				}
			}

			// make sure that latest >= stable
			if semver.Compare(latest.Tag, stable.Tag) == -1 {
				latest.Tag = stable.Tag
			}
			// make sure that edge >= latest
			if semver.Compare(edge.Tag, latest.Tag) == -1 {
				edge.Tag = latest.Tag
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	// create a map where the key is the tag and the values is an array of releases (stable, latest, edge) that match the tag
	releasesWithoutCommits := map[string][]*Release{}
	for _, rl := range []*Release{&stable, &latest, &edge} {
		releasesWithoutCommits[rl.Tag] = append(releasesWithoutCommits[rl.Tag], rl)
	}
	// run though the releases to find ones that don't yet have a commit and assign it
	opts = &github.ListOptions{PerPage: ghListPerPage}
	for (opts.Page+1)*ghListPerPage <= ghSearchLimit {
		tags, resp, err := ghc.Repositories.ListTags(ctx, owner, repo, opts)
		if err != nil {
			return stable, latest, edge, err
		}
		for _, tag := range tags {
			rls, ok := releasesWithoutCommits[*tag.Name]
			if !ok {
				continue
			}
			for _, rl := range rls {
				rl.Commit = *tag.Commit.SHA
			}
			delete(releasesWithoutCommits, *tag.Name)
			if len(releasesWithoutCommits) == 0 {
				return stable, latest, edge, nil
			}
		}
		if len(releasesWithoutCommits) == 0 {
			break
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return stable, latest, edge, fmt.Errorf("wasn't able to find commit for releases")
}

func StableVersion(ctx context.Context, owner, repo string) (string, error) {
	stable, _, _, err := GHReleases(ctx, owner, repo)
	if err != nil || !semver.IsValid(stable.Tag) {
		return "", err
	}
	return stable.Tag, nil
}
