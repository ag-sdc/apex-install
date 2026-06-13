package main

type PackageCandidate struct {
	RepoPriority int
	Repo         *RepoConfig
	Name         string
	Version      string
	Arch         string
	MicroArch    string
	ApiLevel     string
	Depends      []string
	Provides     []string
}

type RegistryCache struct {
	Providers map[string][]string // libName -> list of package names
	Packages  []*PackageCandidate // all packages in this repo
}
