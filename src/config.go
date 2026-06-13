package main

import (
	"bufio"
	"os"
	"strings"
)

type RepoConfig struct {
	Name         string
	URL          string
	AuthToken    string
	AuthBasic    string
	Arch         string
	MaxMicroArch string
	MinMicroArch string
	MaxApiLevel  string
	MinApiLevel  string
}

func readConfig(path string) ([]*RepoConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var repos []*RepoConfig
	var currentRepo *RepoConfig

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentRepo = &RepoConfig{
				Name: line[1 : len(line)-1],
			}
			repos = append(repos, currentRepo)
			continue
		}
		if currentRepo == nil {
			continue // ignore keys outside of sections
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "URL":
			currentRepo.URL = strings.TrimRight(v, "/")
		case "AUTH_TOKEN":
			currentRepo.AuthToken = v
		case "AUTH_BASIC":
			currentRepo.AuthBasic = v
		case "ARCH":
			currentRepo.Arch = v
		case "MAX_MICROARCH":
			currentRepo.MaxMicroArch = v
		case "MIN_MICROARCH":
			currentRepo.MinMicroArch = v
		case "MAX_API_LEVEL":
			currentRepo.MaxApiLevel = v
		case "MIN_API_LEVEL":
			currentRepo.MinApiLevel = v
		}
	}
	return repos, scanner.Err()
}
