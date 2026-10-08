package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	a = strings.TrimSpace(a)
	a = strings.TrimPrefix(a, "API")
	a = strings.TrimPrefix(a, "api")
	a = strings.TrimSpace(a)
	a = strings.TrimPrefix(a, "v")
	if a == "" || a == "0" {
		return __min_supported_abi
	}
	i, err := strconv.Atoi(a)
	if err != nil {
		return __min_supported_abi
	}
	return i
}

func deriveShortAlias(name string) string {
	if !strings.Contains(name, ".") {
		return name
	}
	prefixes := []string{
		"com.google.android.",
		"com.google.mainline.",
		"com.android.",
		"org.blisslabs.ag.",
		"org.blisslabs.",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return strings.TrimPrefix(name, p)
		}
	}
	parts := strings.Split(name, ".")
	if len(parts) >= 3 {
		return strings.Join(parts[2:], ".")
	}
	return name
}

func fetchRepoData(repoIndex int, repo *RepoConfig) (*RegistryCache, error) {
	cache := &RegistryCache{
		Providers:    make(map[string][]string),
		ShortAliases: make(map[string][]string),
	}

	syncDir := repo.SyncDir
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
				// Match package if the arch matches our repo.Arch or if repo.Arch is empty/any
				if repo.Arch == "" || repo.Arch == arch || arch == "any" {
					pkgName := parts[4]
					cache.Providers[lib] = append(cache.Providers[lib], pkgName)
					if strings.HasPrefix(lib, "alias:") {
						shortName := strings.TrimPrefix(lib, "alias:")
						cache.ShortAliases[shortName] = append(cache.ShortAliases[shortName], pkgName)
					}
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
			}
			if len(parts) >= 7 {
				cand.Type = parts[6]
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
				// Auto-register short-name alias from package name
				alias := deriveShortAlias(cand.Name)
				if alias != cand.Name {
					cache.ShortAliases[alias] = append(cache.ShortAliases[alias], cand.Name)
				}
			}
		}
	}

	return cache, nil
}

func searchFiles(pattern string, repos []*RepoConfig) {
	foundAny := false
	for _, repo := range repos {
		syncDir := repo.SyncDir
		filesZst := filepath.Join(syncDir, repo.Name+".files.zst")
		plainFiles := filepath.Join(syncDir, repo.Name+".files")

		var reader io.ReadCloser
		if _, err := os.Stat(filesZst); err == nil {
			cmd := exec.Command("zstd", "-d", "-c", filesZst)
			stdout, err := cmd.StdoutPipe()
			if err == nil && cmd.Start() == nil {
				reader = stdout
				defer cmd.Wait()
			}
		} else if _, err := os.Stat(plainFiles); err == nil {
			f, err := os.Open(plainFiles)
			if err == nil {
				reader = f
			}
		}

		// Try downloading on-demand if not found locally
		if reader == nil {
			LogV("File database missing for %s, attempting on-demand sync...", repo.Name)
			err := tryDownload(repo, syncDir, ".files.zst", []string{".files.zst", ".files.tar.zst", ".files"})
			if err == nil {
				if _, statErr := os.Stat(filesZst); statErr == nil {
					cmd := exec.Command("zstd", "-d", "-c", filesZst)
					stdout, pipeErr := cmd.StdoutPipe()
					if pipeErr == nil && cmd.Start() == nil {
						reader = stdout
						defer cmd.Wait()
					}
				}
			}
		}

		if reader == nil {
			continue
		}

		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 {
				pkgName := parts[0]
				filePath := parts[1]
				if strings.Contains(filePath, pattern) {
					fmt.Printf("%s/%s %s\n", repo.Name, pkgName, filePath)
					foundAny = true
				}
			}
		}
		reader.Close()
	}

	if !foundAny {
		fmt.Printf("No packages found owning file matching '%s'\n", pattern)
	}
}

