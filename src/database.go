package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func parseMicroArch(m string) float64 {
	m = strings.TrimPrefix(m, "v")
	m = strings.TrimSuffix(m, "a") // Handle cases like 8.2a
	f, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0
	}
	return f
}

var __min_supported_abi = 29

func parseApiLevel(a string) int {
	if a == "" {
		return __min_supported_abi
	}
	i, err := strconv.Atoi(a)
	if err != nil {
		return __min_supported_abi
	}
	return i
}

func fetchRepoData(repoIndex int, repo *RepoConfig) (*RegistryCache, error) {
	cache := &RegistryCache{
		Providers: make(map[string][]string),
	}

	syncDir := "/var/cache/apex/sync"
	if _, err := os.Stat(syncDir); os.IsNotExist(err) {
		os.MkdirAll(syncDir, 0755)
	}

	// 1. Fetch Providers
	provFile := filepath.Join(syncDir, repo.Name+".providers")
	if f, err := os.Open(provFile); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			parts := strings.Fields(line)
			if len(parts) >= 6 {
				lib := parts[0]
				arch := parts[1]
				// We match the package if the arch matches our repo.Arch or if repo.Arch is empty/any
				if repo.Arch == "" || repo.Arch == arch || arch == "any" {
					pkgName := parts[4]
					cache.Providers[lib] = append(cache.Providers[lib], pkgName)
				}
			}
		}
		f.Close()
	}

	// 2. Fetch DB
	dbFile := filepath.Join(syncDir, repo.Name+".db")
	var dbReader io.ReadCloser
	if f, err := os.Open(dbFile); err == nil {
		dbReader = f
	} else {
		return nil, fmt.Errorf("repository database missing locally, run with --update first")
	}
	defer dbReader.Close()

	scanner := bufio.NewScanner(dbReader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 6 {
			cand := &PackageCandidate{
				RepoPriority: repoIndex,
				Repo:         repo,
				Name:         parts[0],
				Arch:         parts[1],
				MicroArch:    parts[2],
				ApiLevel:     parts[3],
				Version:      parts[4],
				// Size is parts[5], but we don't strictly need it right now for the candidate struct
			}

			if repo.MinMicroArch != "" && parseMicroArch(cand.MicroArch) < parseMicroArch(repo.MinMicroArch) {
				continue
			}
			if repo.MaxMicroArch != "" && parseMicroArch(cand.MicroArch) > parseMicroArch(repo.MaxMicroArch) {
				continue
			}
			if repo.MinApiLevel != "" && parseApiLevel(cand.ApiLevel) < parseApiLevel(repo.MinApiLevel) {
				continue
			}
			if repo.MaxApiLevel != "" && parseApiLevel(cand.ApiLevel) > parseApiLevel(repo.MaxApiLevel) {
				continue
			}

			if repo.Arch == "" || repo.Arch == cand.Arch || cand.Arch == "any" {
				cache.Packages = append(cache.Packages, cand)
			}
		}
	}

	return cache, nil
}
