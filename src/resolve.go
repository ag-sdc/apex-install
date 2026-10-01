package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

func sortCandidates(candidates []*PackageCandidate, maxMicroarch string) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RepoPriority != candidates[j].RepoPriority {
			return candidates[i].RepoPriority < candidates[j].RepoPriority
		}
		mI := parseMicroArch(candidates[i].MicroArch)
		mJ := parseMicroArch(candidates[j].MicroArch)
		if mI != mJ {
			if maxMicroarch != "" {
				return mI > mJ // highest first
			}
			return mI < mJ // lowest first
		}

		apiI := parseApiLevel(candidates[i].ApiLevel)
		apiJ := parseApiLevel(candidates[j].ApiLevel)
		if apiI != apiJ {
			return apiI > apiJ
		}

		cmp := compareVersions(candidates[i].Version, candidates[j].Version)
		if cmp != 0 {
			return cmp > 0
		}
		return candidates[i].Version > candidates[j].Version
	})
}

func matchCandidateApi(cand *PackageCandidate, apiLevelExact, apiLevel int) bool {
	if apiLevelExact > 0 {
		return parseApiLevel(cand.ApiLevel) == apiLevelExact
	}
	if apiLevel > 0 {
		return parseApiLevel(cand.ApiLevel) <= apiLevel
	}
	return true
}

func matchInstalledPkgApi(pkg *installedPkgInfo, apiLevelExact, apiLevel int) bool {
	if apiLevelExact > 0 {
		return pkg.apiLevel != "" && parseApiLevel(pkg.apiLevel) == apiLevelExact
	}
	if apiLevel > 0 {
		return pkg.apiLevel != "" && parseApiLevel(pkg.apiLevel) <= apiLevel
	}
	return true
}

func resolveExtension(cand *PackageCandidate) string {
	if cand.Type != "" {
		return cand.Type
	}

	dirMicroArch := strings.ReplaceAll(cand.MicroArch, "_", ".")
	archSegment := cand.Arch
	if dirMicroArch != "" {
		archSegment = fmt.Sprintf("%s/v%s", cand.Arch, dirMicroArch)
	}

	apiLevelSegment := cand.ApiLevel
	if apiLevelSegment == "" || apiLevelSegment == "0" {
		apiLevelSegment = "29"
	}

	orgPath := strings.ReplaceAll(cand.Name, ".", "/")
	pkgFilename := fmt.Sprintf("%s.capex", cand.Version)
	urlStr := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)

	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return "apex"
	}
	if cand.Repo.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cand.Repo.AuthToken)
	} else if cand.Repo.AuthBasic != "" {
		req.Header.Set("Authorization", "Basic "+cand.Repo.AuthBasic)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "apex"
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		pkgFilename = fmt.Sprintf("%s.apex", cand.Version)
		urlStr = fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)
		req, _ = http.NewRequest("HEAD", urlStr, nil)
		if cand.Repo.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+cand.Repo.AuthToken)
		} else if cand.Repo.AuthBasic != "" {
			req.Header.Set("Authorization", "Basic "+cand.Repo.AuthBasic)
		}
		resp2, err := client.Do(req)
		if err == nil {
			defer resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				return "apex"
			}
		}
	case http.StatusOK:
		return "capex"
	}
	return "apex"
}
