package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	archFlag := flag.String("arch", "", "Target architecture (required if --max-microarch is set)")
	maxMicroarch := flag.String("max-microarch", "", "Highest microarchitecture level to download (prioritizes higher microarch)")
	apiLevel := flag.Int("api-level", 0, "Highest API level to download (prioritizes higher api-level, min 29)")
	searchOnly := flag.Bool("search", false, "Search only, do not install or resolve dependencies")
	updateFlag := flag.Bool("update", false, "Update local repository databases")
	flag.Parse()

	if *maxMicroarch != "" && *archFlag == "" {
		fmt.Println("Error: --max-microarch requires the --arch flag")
		flag.PrintDefaults()
		os.Exit(1)
	}

	targets := flag.Args()
	if len(targets) == 0 && !*updateFlag {
		fmt.Println("Usage: apex-install [options] <target1> [target2] ...")
		flag.PrintDefaults()
		os.Exit(1)
	}

	repos, err := readConfig("/etc/apex/repo.conf")
	if err != nil || len(repos) == 0 {
		repos, err = readConfig("repo.conf")
		if err != nil || len(repos) == 0 {
			fmt.Println("Failed to read config or no repositories defined")
			os.Exit(1)
		}
	}

	if *updateFlag {
		doUpdate(repos)
		if len(targets) == 0 {
			os.Exit(0)
		}
	}

	if !*searchOnly {
		fmt.Fprintln(os.Stderr, "Fetching repository metadata...")
	}
	caches := make([]*RegistryCache, len(repos))
	for i, repo := range repos {
		cache, err := fetchRepoData(i, repo)
		if err != nil {
			if !*searchOnly {
				fmt.Fprintf(os.Stderr, "Warning: Failed to fetch metadata for repo %s: %v\n", repo.Name, err)
			}
			continue
		}
		caches[i] = cache
	}

	queue := append([]string{}, targets...)
	resolved := make(map[string]bool)
	var installList []*PackageCandidate

	for len(queue) > 0 {
		target := queue[0]
		queue = queue[1:]

		if IsSatisfiedLib(target) {
			continue // skip satisfied system libs
		}
		if resolved[target] {
			continue
		}

		var candidates []*PackageCandidate

		// Search across all caches
		for _, cache := range caches {
			if cache == nil {
				continue
			}

			// If target is a library, resolve it to package names
			searchPkgs := []string{target}

			for _, sp := range searchPkgs {
				if pkgs, ok := cache.Providers[sp]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
			}

			// Find packages matching the search names
			for _, pkgName := range searchPkgs {
				for _, cand := range cache.Packages {
					if cand.Name == pkgName {
						if *maxMicroarch != "" && parseMicroArch(cand.MicroArch) > parseMicroArch(*maxMicroarch) {
							continue // skip if higher than requested maximum
						}
						if *apiLevel > 0 {
							cApi := parseApiLevel(cand.ApiLevel)
							if cApi > *apiLevel {
								continue // skip if higher than target API level
							}
						}
						candidates = append(candidates, cand)
					}
				}
			}
		}

		if len(candidates) == 0 {
			fmt.Printf("Error: Unresolvable dependency: %s\n", target)
			os.Exit(1)
		}

		sortCandidates(candidates, *maxMicroarch)
		selected := candidates[0]

		if *searchOnly {
			ext := resolveExtension(selected)
			microarchStr := selected.MicroArch
			if !strings.HasPrefix(microarchStr, "v") {
				microarchStr = "v" + microarchStr
			}
			fmt.Printf("%s.%s %s %s-%s\n", selected.Name, ext, selected.Version, selected.Arch, microarchStr)
			continue // do not add to installList and don't resolve dependencies
		}

		fmt.Printf("Resolved %s -> %s.%s v%s (Repo: %s)\n", target, selected.Name, resolveExtension(selected), selected.Version, selected.Repo.Name)

		installList = append(installList, selected)
		resolved[target] = true
		resolved[selected.Name] = true

		// Add dependencies to queue
		for _, dep := range selected.Depends {
			if !resolved[dep] {
				queue = append(queue, dep)
			}
		}
	}

	if *searchOnly {
		return // we just exit
	}

	fmt.Printf("\nReady to install %d packages.\n", len(installList))
	for _, pkg := range installList {
		fmt.Printf("Installing %s v%s...\n", pkg.Name, pkg.Version)
		if err := downloadAndExtract(pkg); err != nil {
			fmt.Printf("Installation failed for %s: %v\n", pkg.Name, err)
			os.Exit(1)
		}
	}

	fmt.Println("All packages installed and mounted successfully!")
}
