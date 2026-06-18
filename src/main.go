package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

var ActiveConfig ContextConfig
var VerboseMode bool
var DownloadOnlyMode bool
var Sysconfdir string = "/etc"

func LogV(format string, args ...interface{}) {
	if VerboseMode {
		fmt.Printf(format, args...)
		if !strings.HasSuffix(format, "\n") {
			fmt.Println()
		}
	}
}

func preProcessArgs() {
	if len(os.Args) < 2 {
		return
	}
	cmd := os.Args[1]
	if !strings.HasPrefix(cmd, "-") {
		switch cmd {
		case "install":
			os.Args[1] = "-S"
		case "remove":
			os.Args[1] = "-R"
		case "local":
			os.Args[1] = "-Q"
		case "init":
			os.Args[1] = "-i"
		}
	}
}

func handleInit() {
	LogV("Re-initializing installed APEXes...")
	entries, err := os.ReadDir(ActiveConfig.DownloadPath)
	if err != nil {
		fmt.Printf("Failed to read download path: %v\n", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			pkgName := strings.TrimSuffix(entry.Name(), ".apex")
			if strings.HasSuffix(entry.Name(), ".capex") {
				pkgName = strings.TrimSuffix(entry.Name(), ".capex")
			}
			targetDir := filepath.Join(ActiveConfig.DownloadPath, entry.Name())
			payloadImg := filepath.Join(targetDir, "apex_payload.img")
			if _, err := os.Stat(payloadImg); err == nil {
				LogV("Initializing %s...", pkgName)
				if err := installLocalApex(pkgName, targetDir); err != nil {
					fmt.Printf("Failed to initialize %s: %v\n", pkgName, err)
				}
			}
		}
	}
}

func handleLocal(libName string, nameOnly bool) {
	entries, err := os.ReadDir(ActiveConfig.InstallPath)
	if err != nil {
		if !nameOnly {
			fmt.Printf("Failed to read install path: %v\n", err)
		}
		os.Exit(1)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pkgName := entry.Name()
		for _, libDir := range []string{"lib", "lib64"} {
			libPath := filepath.Join(ActiveConfig.InstallPath, pkgName, libDir)
			libs, err := os.ReadDir(libPath)
			if err != nil {
				continue
			}
			for _, lib := range libs {
				if strings.Contains(lib.Name(), libName) {
					if nameOnly {
						fmt.Println(pkgName)
					} else {
						fmt.Printf("Found in: %s (at %s)\n", pkgName, filepath.Join(libPath, lib.Name()))
					}
					os.Exit(0)
				}
			}
		}
	}
	os.Exit(1)
}

func main() {
	preProcessArgs()

	pflag.CommandLine.SortFlags = false

	configFlag := pflag.StringP("config", "c", "", "Config file path")
	verboseFlag := pflag.BoolP("verbose", "v", false, "Verbose logging")
	
	installMode := pflag.BoolP("install", "S", false, "Install packages")
	removeMode := pflag.BoolP("remove", "R", false, "Remove packages")
	localMode := pflag.BoolP("local", "Q", false, "Search installed packages for a library name")
	initMode := pflag.Bool("init", false, "Re-mount and symlink all installed APEXes")

	isolatedFlag := pflag.BoolP("isolated", "i", false, "Isolated mode: ignore system states (user) or operate on rootdir (system)")
	rootDirFlag := pflag.StringP("rootdir", "r", "", "Root directory for isolated system operations")

	updateFlag := pflag.BoolP("update", "u", false, "Update local repository databases (and override installed)")
	searchFlag := pflag.BoolP("search", "s", false, "Search only, do not install or resolve dependencies")
	downloadFlag := pflag.BoolP("download", "d", false, "Download only, do not extract or mount")

	nameFlag := pflag.Bool("name", false, "Show only the package name when using -Q")
	archFlag := pflag.String("arch", "", "Target architecture (required if --max-microarch is set)")
	maxMicroarch := pflag.String("max-microarch", "", "Highest microarchitecture level to download (prioritizes higher microarch)")
	apiLevel := pflag.Int("api-level", 0, "Highest API level to download (prioritizes higher api-level, min 29)")
	
	pflag.ErrHelp = fmt.Errorf("pflag: help requested")
	pflag.Parse()

	modesActive := 0
	if *installMode { modesActive++ }
	if *removeMode { modesActive++ }
	if *localMode { modesActive++ }
	if *initMode { modesActive++ }

	if modesActive > 1 {
		fmt.Println("Error: only one operation may be used at a time")
		os.Exit(1)
	}

	targets := pflag.Args()
	if modesActive == 0 && !*updateFlag && !*searchFlag {
		fmt.Println("Usage: apexm [operation] [options] <target1> [target2] ...")
		pflag.PrintDefaults()
		os.Exit(1)
	}

	var apexConfig *ApexConfig
	var err error
	
	defaultGlobalConfig := filepath.Join(Sysconfdir, "apex", "apex.conf")
	isRoot := os.Geteuid() == 0

	if !isRoot && *isolatedFlag {
		apexConfig, err = parseApexConfig("~/.config/apex/apex.conf", nil)
	} else {
		apexConfig, err = parseApexConfig(defaultGlobalConfig, nil)
		if !isRoot {
			apexConfig, _ = parseApexConfig("~/.config/apex/apex.conf", apexConfig)
		}
	}

	if *configFlag != "" {
		apexConfig, err = parseApexConfig(*configFlag, apexConfig)
	}

	if err != nil {
		fmt.Printf("Warning: Failed to parse apex.conf: %v\n", err)
	}

	if isRoot {
		ActiveConfig = apexConfig.Root
		if *isolatedFlag {
			if *rootDirFlag == "" {
				fmt.Println("Error: --rootdir/-r must be provided when running as root with --isolated")
				os.Exit(1)
			}
			
			// Copy system configs into rootDir
			os.MkdirAll(filepath.Join(*rootDirFlag, filepath.Dir(defaultGlobalConfig)), 0755)
			copyFile(defaultGlobalConfig, filepath.Join(*rootDirFlag, defaultGlobalConfig))
			os.MkdirAll(filepath.Join(*rootDirFlag, filepath.Dir(ActiveConfig.RepoPath)), 0755)
			copyFile(ActiveConfig.RepoPath, filepath.Join(*rootDirFlag, ActiveConfig.RepoPath))

			ActiveConfig.InstallPath = filepath.Join(*rootDirFlag, "apex")
			ActiveConfig.DownloadPath = filepath.Join(*rootDirFlag, "opt", "apex")
			ActiveConfig.MergePath = filepath.Join(*rootDirFlag, "apex")
			ActiveConfig.DBCacheDir = filepath.Join(*rootDirFlag, "var", "cache", "apex", "sync")
			ActiveConfig.RepoPath = filepath.Join(*rootDirFlag, ActiveConfig.RepoPath)
		}
	} else {
		ActiveConfig = apexConfig.User
	}
	ActiveConfig.DownloadPath = expandTilde(ActiveConfig.DownloadPath)
	ActiveConfig.InstallPath = expandTilde(ActiveConfig.InstallPath)
	ActiveConfig.MergePath = expandTilde(ActiveConfig.MergePath)
	ActiveConfig.DBCacheDir = expandTilde(ActiveConfig.DBCacheDir)
	ActiveConfig.RepoPath = expandTilde(ActiveConfig.RepoPath)

	VerboseMode = *verboseFlag
	DownloadOnlyMode = *downloadFlag

	if *initMode {
		handleInit()
		os.Exit(0)
	}

	if *localMode {
		if len(targets) == 0 {
			fmt.Println("Error: no library name specified for local query")
			os.Exit(1)
		}
		handleLocal(targets[0], *nameFlag)
	}

	if *removeMode {
		if len(targets) == 0 {
			fmt.Println("Error: no targets specified for removal")
			os.Exit(1)
		}
		for _, t := range targets {
			if err := uninstallApex(t); err != nil {
				fmt.Printf("Error removing %s: %v\n", t, err)
			} else {
				fmt.Printf("Successfully removed %s\n", t)
			}
		}
		os.Exit(0)
	}

	if *maxMicroarch == "" && ActiveConfig.MaxMicroArch != "" {
		*maxMicroarch = ActiveConfig.MaxMicroArch
	}
	if *apiLevel == 0 && ActiveConfig.MaxApiLevel != "" {
		if parsedApi, err := strconv.Atoi(ActiveConfig.MaxApiLevel); err == nil && parsedApi > 0 {
			*apiLevel = parsedApi
		}
	}

	if *maxMicroarch != "" && *archFlag == "" {
		fmt.Println("Error: --max-microarch requires the --arch flag")
		pflag.PrintDefaults()
		os.Exit(1)
	}

	repos, err := readRepoConfig(ActiveConfig.RepoPath, ActiveConfig.DBCacheDir)
	if err != nil || len(repos) == 0 {
		repos, err = readRepoConfig("repo.conf", ActiveConfig.DBCacheDir)
		if err != nil || len(repos) == 0 {
			fmt.Println("Failed to read repo config or no repositories defined")
			os.Exit(1)
		}
	}

	if !isRoot && !*isolatedFlag {
		if systemRepos, err := readRepoConfig(apexConfig.Root.RepoPath, apexConfig.Root.DBCacheDir); err == nil {
			repos = append(repos, systemRepos...)
		}
	}

	if *updateFlag {
		doUpdate(repos)
		if len(targets) == 0 {
			os.Exit(0)
		}
	}

	if !*searchFlag {
		fmt.Fprintln(os.Stderr, "Fetching repository metadata...")
	}
	caches := make([]*RegistryCache, len(repos))
	for i, repo := range repos {
		cache, err := fetchRepoData(i, repo)
		if err != nil {
			if !*searchFlag {
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

		if *searchFlag {
			ext := resolveExtension(selected)
			microarchStr := selected.MicroArch
			if !strings.HasPrefix(microarchStr, "v") {
				microarchStr = "v" + microarchStr
			}
			fmt.Printf("%s.%s %s %s-%s\n", selected.Name, ext, selected.Version, selected.Arch, microarchStr)
			continue // do not add to installList and don't resolve dependencies
		}

		LogV("Resolved %s -> %s.%s v%s (Repo: %s)", target, selected.Name, resolveExtension(selected), selected.Version, selected.Repo.Name)

		if resolved[selected.Name] {
			resolved[target] = true
			continue
		}

		// Check if installed locally
		mountPoint := filepath.Join(ActiveConfig.InstallPath, selected.Name)
		if _, err := os.Stat(mountPoint); err == nil {
			LogV("Package %s is already installed. Skipping.", selected.Name)
			continue // do not add to installList and don't resolve dependencies
		}

		if !isRoot && !*isolatedFlag {
			sysMountPoint := filepath.Join(apexConfig.Root.InstallPath, selected.Name)
			if _, err := os.Stat(sysMountPoint); err == nil {
				LogV("Package %s is already installed in system. Skipping.", selected.Name)
				continue
			}
		}

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

	if *searchFlag {
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

	fmt.Println("All packages installed successfully!")
}
