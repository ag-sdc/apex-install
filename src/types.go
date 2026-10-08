package main

type PackageCandidate struct {
	RepoPriority int
	Repo         *RepoConfig
	Name         string
	Version      string
	Arch         string
	MicroArch    string
	ApiLevel     string
	Type         string
	Depends      []string
	Provides     []string
}

type RegistryCache struct {
	Providers    map[string][]string // libName / capability / alias -> list of package names
	ShortAliases map[string][]string // shortName -> list of package names
	Packages     []*PackageCandidate // all packages in this repo
}

