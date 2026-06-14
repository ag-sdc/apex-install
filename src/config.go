package main

import (
	"bufio"
	"os"
	"path/filepath"
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

type ContextConfig struct {
	MountMode    string
	MaxMicroArch string
	MinMicroArch string
	MaxApiLevel  string
	MinApiLevel  string
	DownloadPath string
	InstallPath  string
	MergePath    string
	DBCacheDir   string
	RepoPath     string
}

type ApexConfig struct {
	Root ContextConfig
	User ContextConfig
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func parseApexConfig(path string, baseConfig *ApexConfig) (*ApexConfig, error) {
	var config *ApexConfig
	if baseConfig != nil {
		config = baseConfig
	} else {
		config = &ApexConfig{
			Root: ContextConfig{
				MountMode:    "auto",
				DownloadPath: "/opt/apex",
				InstallPath:  "/apex",
				MergePath:    "/apex",
				DBCacheDir:   "/var/cache/apex/sync",
				RepoPath:     filepath.Join(Sysconfdir, "apex", "repo.conf"),
			},
			User: ContextConfig{
				MountMode:    "auto",
				DownloadPath: "~/.apex/opt",
				InstallPath:  "~/.apex/mnt",
				MergePath:    "~/.apex",
				DBCacheDir:   "~/.cache/apex/sync",
				RepoPath:     "~/.config/apex/repo.conf",
			},
		}
	}

	file, err := os.Open(expandTilde(path))
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return current config if file not found
		}
		return config, err
	}
	defer file.Close()

	var currentSection string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line[1 : len(line)-1]
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])

		switch currentSection {
		case "root":
			switch k {
			case "mount_mode":
				config.Root.MountMode = v
			case "max_api_level":
				config.Root.MaxApiLevel = v
			case "min_api_level":
				config.Root.MinApiLevel = v
			case "max_microarch":
				config.Root.MaxMicroArch = v
			case "min_microarch":
				config.Root.MinMicroArch = v
			case "download_path":
				config.Root.DownloadPath = v
			case "install_path":
				config.Root.InstallPath = v
			case "merge_path":
				config.Root.MergePath = v
			case "db_cache_dir":
				config.Root.DBCacheDir = v
			case "repo_path":
				config.Root.RepoPath = v
			}
		case "user":
			switch k {
			case "mount_mode":
				config.User.MountMode = v
			case "max_api_level":
				config.User.MaxApiLevel = v
			case "min_api_level":
				config.User.MinApiLevel = v
			case "max_microarch":
				config.User.MaxMicroArch = v
			case "min_microarch":
				config.User.MinMicroArch = v
			case "download_path":
				config.User.DownloadPath = v
			case "install_path":
				config.User.InstallPath = v
			case "merge_path":
				config.User.MergePath = v
			case "db_cache_dir":
				config.User.DBCacheDir = v
			case "repo_path":
				config.User.RepoPath = v
			}
		}
	}

	return config, scanner.Err()
}

func readRepoConfig(path string) ([]*RepoConfig, error) {
	file, err := os.Open(expandTilde(path))
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
			continue
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
