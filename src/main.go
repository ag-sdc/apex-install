package main

import (
	"bufio"
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"golang.org/x/mod/semver"
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
var NoConfirmMode bool
var Sysconfdir string = "/etc"

func LogV(format string, args ...interface{}) {
	if VerboseMode {
		fmt.Printf(format, args...)
		if !strings.HasSuffix(format, "\n") {
			fmt.Println()
		}
	}
}

func isValFlag(arg string) bool {
	switch arg {
	case "-c", "--config", "-r", "--rootdir", "--arch", "--max-microarch",
		"--api-level", "--api", "--api-level-exact", "--api-exact":
		return true
	}
	return false
}

func isOpFlag(arg string) bool {
	if arg == "--install" || arg == "--remove" || arg == "--local" || arg == "--init" || arg == "--list" {
		return true
	}
	if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
		for _, r := range arg[1:] {
			if r == 'S' || r == 'R' || r == 'Q' {
				return true
			}
		}
	}
	return false
}

func preProcessArgs() {
	if len(os.Args) < 2 {
		return
	}

	hasOp := false
	for _, arg := range os.Args[1:] {
		if isOpFlag(arg) {
			hasOp = true
			break
		}
	}

	if !hasOp {
		subcmdIdx := -1
		for i := 1; i < len(os.Args); i++ {
			arg := os.Args[i]
			if strings.HasPrefix(arg, "-") {
				if isValFlag(arg) && !strings.Contains(arg, "=") {
					i++
				}
				continue
			}
			switch arg {
			case "search", "install", "remove", "init", "update", "download", "local", "list":
				subcmdIdx = i
			}
			break
		}

		if subcmdIdx != -1 {
			cmd := os.Args[subcmdIdx]
			switch cmd {
			case "search":
				isLocal := false
				var tail []string
				for _, a := range os.Args[subcmdIdx+1:] {
					if a == "-Q" || a == "--local" || a == "local" || a == "-Qs" {
						isLocal = true
					} else {
						tail = append(tail, a)
					}
				}
				for _, a := range os.Args[:subcmdIdx] {
					if a == "-Q" || a == "--local" || a == "-Qs" {
						isLocal = true
					}
				}
				prefix := append([]string{}, os.Args[:subcmdIdx]...)
				if isLocal {
					os.Args = append(append(prefix, "-Q", "-s"), tail...)
				} else {
					os.Args = append(append(prefix, "-S", "-s"), tail...)
				}
				return

			case "local":
				isSearch := false
				var tail []string
				for _, a := range os.Args[subcmdIdx+1:] {
					if a == "search" || a == "-s" || a == "--search" || a == "-Qs" {
						isSearch = true
					} else {
						tail = append(tail, a)
					}
				}
				for _, a := range os.Args[:subcmdIdx] {
					if a == "-s" || a == "--search" || a == "-Qs" {
						isSearch = true
					}
				}
				prefix := append([]string{}, os.Args[:subcmdIdx]...)
				if isSearch {
					os.Args = append(append(prefix, "-Q", "-s"), tail...)
				} else {
					os.Args = append(append(prefix, "-Q"), tail...)
				}
				return

			case "install":
				os.Args[subcmdIdx] = "-S"
				return
			case "remove":
				os.Args[subcmdIdx] = "-R"
				return
			case "init":
				os.Args[subcmdIdx] = "--init"
				return
			case "list":
				os.Args[subcmdIdx] = "--list"
				return
			case "update":
				tail := append([]string{}, os.Args[subcmdIdx+1:]...)
				prefix := append([]string{}, os.Args[:subcmdIdx]...)
				os.Args = append(append(prefix, "-S", "-u"), tail...)
				return
			case "download":
				tail := append([]string{}, os.Args[subcmdIdx+1:]...)
				prefix := append([]string{}, os.Args[:subcmdIdx]...)
				os.Args = append(append(prefix, "-S", "-d"), tail...)
				return
			}
		}

		// Handle bare -s or --search without -S or -Q
		hasSearch := false
		isLocal := false
		for _, a := range os.Args[1:] {
			if a == "-s" || a == "--search" {
				hasSearch = true
			}
			if a == "-Q" || a == "--local" {
				isLocal = true
			}
		}
		if hasSearch && !isLocal {
			os.Args = append([]string{os.Args[0], "-S"}, os.Args[1:]...)
			return
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

	type downloadedPkg struct {
		baseName    string
		versionCode string
		dirName     string
		targetDir   string
	}

	bestPkgs := make(map[string]downloadedPkg)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		var rawName string
		if strings.HasSuffix(dirName, ".apex") {
			rawName = strings.TrimSuffix(dirName, ".apex")
		} else if strings.HasSuffix(dirName, ".capex") {
			rawName = strings.TrimSuffix(dirName, ".capex")
		} else {
			continue
		}

		targetDir := filepath.Join(ActiveConfig.DownloadPath, dirName)
		payloadImg := filepath.Join(targetDir, "apex_payload.img")
		if _, err := os.Stat(payloadImg); err != nil {
			continue
		}

		baseName := rawName
		versionCode := ""
		if idx := strings.LastIndex(rawName, "@"); idx > 0 {
			baseName = rawName[:idx]
			versionCode = rawName[idx+1:]
		}

		pkg := downloadedPkg{
			baseName:    baseName,
			versionCode: versionCode,
			dirName:     dirName,
			targetDir:   targetDir,
		}

		existing, found := bestPkgs[baseName]
		if !found {
			bestPkgs[baseName] = pkg
		} else {
			if compareVersions(versionCode, existing.versionCode) > 0 {
				bestPkgs[baseName] = pkg
			}
		}
	}

	for baseName, pkg := range bestPkgs {
		LogV("Initializing %s (versionCode %s)...", baseName, pkg.versionCode)
		if err := installLocalApex(baseName, pkg.targetDir); err != nil {
			fmt.Printf("Failed to initialize %s: %v\n", baseName, err)
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

func handleList() {
	entries, err := os.ReadDir(ActiveConfig.InstallPath)
	if err != nil {
		fmt.Printf("No packages installed (cannot read %s)\n", ActiveConfig.InstallPath)
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			fmt.Println(entry.Name())
			count++
		}
	}
	if count == 0 {
		fmt.Println("No packages installed.")
	}
}

func parseTarget(target string) (name, versionCode string) {
	if strings.HasPrefix(target, "@") {
		return "", target[1:]
	}
	if idx := strings.LastIndex(target, "@"); idx > 0 {
		return target[:idx], target[idx+1:]
	}
	return target, ""
}

func canonicalizeSemver(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	parts := strings.SplitN(v[1:], "-", 2)
	dots := strings.Count(parts[0], ".")
	if dots == 0 {
		parts[0] = parts[0] + ".0.0"
	} else if dots == 1 {
		parts[0] = parts[0] + ".0"
	}
	res := "v" + parts[0]
	if len(parts) == 2 {
		res += "-" + parts[1]
	}
	return res
}

func compareVersions(v1, v2 string) int {
	clean1 := strings.TrimPrefix(v1, "v")
	clean2 := strings.TrimPrefix(v2, "v")
	i1, err1 := strconv.ParseInt(clean1, 10, 64)
	i2, err2 := strconv.ParseInt(clean2, 10, 64)
	if err1 == nil && err2 == nil {
		if i1 > i2 {
			return 1
		} else if i1 < i2 {
			return -1
		}
		return 0
	}
	sv1 := canonicalizeSemver(v1)
	sv2 := canonicalizeSemver(v2)
	if semver.IsValid(sv1) && semver.IsValid(sv2) {
		return semver.Compare(sv1, sv2)
	}
	return strings.Compare(v1, v2)
}

type installedPkgInfo struct {
	name        string
	installDir  string
	libs        []string
	version     string
	versionCode string
	pkgType     string
	arch        string
	microArch   string
	apiLevel    string
}

type apexManifestRaw struct {
	Name        string      `json:"name"`
	Version     interface{} `json:"version"`
	VersionName string      `json:"versionName"`
}

func detectPkgArch(installDir string) string {
	for _, libDir := range []string{"lib64", "lib"} {
		libPath := filepath.Join(installDir, libDir)
		entries, err := os.ReadDir(libPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fPath := filepath.Join(libPath, entry.Name())
			ef, err := elf.Open(fPath)
			if err != nil {
				continue
			}
			mach := ef.Machine
			ef.Close()
			switch mach {
			case elf.EM_X86_64:
				return "x86_64"
			case elf.EM_AARCH64:
				return "aarch64"
			case elf.EM_386:
				return "x86"
			case elf.EM_ARM:
				return "arm"
			case elf.EM_RISCV:
				return "riscv64"
			}
		}
	}
	return ""
}

func formatLocalPackage(pkg *installedPkgInfo) string {
	ext := pkg.pkgType
	if ext == "" {
		ext = "apex"
	}
	microarchStr := pkg.microArch
	if microarchStr != "" && !strings.HasPrefix(microarchStr, "v") {
		microarchStr = "v" + microarchStr
	}

	var details []string
	if ext != "" {
		details = append(details, ext)
	}
	if pkg.arch != "" {
		if microarchStr != "" && microarchStr != "v" {
			details = append(details, fmt.Sprintf("%s-%s", pkg.arch, microarchStr))
		} else {
			details = append(details, pkg.arch)
		}
	}
	if pkg.apiLevel != "" && pkg.apiLevel != "0" {
		details = append(details, fmt.Sprintf("API %s", pkg.apiLevel))
	}

	detailsStr := ""
	if len(details) > 0 {
		detailsStr = " (" + strings.Join(details, ", ") + ")"
	}

	ver := pkg.version
	if ver == "" && pkg.versionCode != "" {
		ver = pkg.versionCode
	}

	if ver != "" {
		return fmt.Sprintf("local/%s %s%s", pkg.name, ver, detailsStr)
	}
	return fmt.Sprintf("local/%s%s", pkg.name, detailsStr)
}

func handleLocalSearch(targets []string, nameOnly, libOnly bool, rootInstall, rootDownload, rootCache string, apiLevelExact, apiLevel int) {
	installPaths := []string{ActiveConfig.InstallPath}
	if rootInstall != "" && expandTilde(rootInstall) != ActiveConfig.InstallPath {
		if _, err := os.Stat(expandTilde(rootInstall)); err == nil {
			installPaths = append(installPaths, expandTilde(rootInstall))
		}
	}
	downloadPaths := []string{ActiveConfig.DownloadPath}
	if rootDownload != "" && expandTilde(rootDownload) != ActiveConfig.DownloadPath {
		if _, err := os.Stat(expandTilde(rootDownload)); err == nil {
			downloadPaths = append(downloadPaths, expandTilde(rootDownload))
		}
	}
	dbCacheDirs := []string{ActiveConfig.DBCacheDir}
	if rootCache != "" && expandTilde(rootCache) != ActiveConfig.DBCacheDir {
		if _, err := os.Stat(expandTilde(rootCache)); err == nil {
			dbCacheDirs = append(dbCacheDirs, expandTilde(rootCache))
		}
	}

	installedMap := make(map[string]*installedPkgInfo)
	var pkgNames []string

	for _, iPath := range installPaths {
		entries, err := os.ReadDir(iPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pkgName := entry.Name()
			if _, exists := installedMap[pkgName]; exists {
				continue
			}
			info := &installedPkgInfo{
				name:       pkgName,
				installDir: filepath.Join(iPath, pkgName),
				pkgType:    "apex",
			}
			installedMap[pkgName] = info
			pkgNames = append(pkgNames, pkgName)
		}
	}

	if len(pkgNames) == 0 {
		if len(targets) == 0 && !nameOnly {
			fmt.Println("No packages installed.")
		}
		os.Exit(1)
	}

	// 1. Collect provided libraries & detect arch via ELF
	for _, pkg := range installedMap {
		libSet := make(map[string]bool)
		for _, libDir := range []string{"lib", "lib64"} {
			libPath := filepath.Join(pkg.installDir, libDir)
			filepath.WalkDir(libPath, func(path string, d os.DirEntry, err error) error {
				if err != nil || d == nil {
					return nil
				}
				if !d.IsDir() {
					libName := d.Name()
					if !libSet[libName] {
						libSet[libName] = true
						pkg.libs = append(pkg.libs, libName)
					}
				}
				return nil
			})
		}
		pkg.arch = detectPkgArch(pkg.installDir)
	}

	// 2. Read apex_manifest.json from installDir or downloadPaths
	for _, pkg := range installedMap {
		manifestPaths := []string{
			filepath.Join(pkg.installDir, "apex_manifest.json"),
		}
		for _, dlPath := range downloadPaths {
			manifestPaths = append(manifestPaths,
				filepath.Join(dlPath, pkg.name+".apex", "apex_manifest.json"),
				filepath.Join(dlPath, pkg.name+".capex", "apex_manifest.json"),
			)
			if matches, err := filepath.Glob(filepath.Join(dlPath, pkg.name+"@*.apex", "apex_manifest.json")); err == nil {
				manifestPaths = append(manifestPaths, matches...)
			}
			if matches, err := filepath.Glob(filepath.Join(dlPath, pkg.name+"@*.capex", "apex_manifest.json")); err == nil {
				manifestPaths = append(manifestPaths, matches...)
			}
		}
		for _, mp := range manifestPaths {
			data, err := os.ReadFile(mp)
			if err != nil {
				continue
			}
			var m apexManifestRaw
			if err := json.Unmarshal(data, &m); err == nil {
				if m.VersionName != "" {
					pkg.version = m.VersionName
				}
				if m.Version != nil {
					var vCode string
					switch v := m.Version.(type) {
					case float64:
						vCode = strconv.FormatInt(int64(v), 10)
					case string:
						vCode = v
					}
					pkg.versionCode = vCode
					if pkg.version == "" {
						pkg.version = vCode
					}
				}
				if pkg.version != "" {
					break
				}
			}
		}
	}

	// 3. Collect version and package type from download paths (fallback / enrich)
	for _, dlPath := range downloadPaths {
		dlEntries, err := os.ReadDir(dlPath)
		if err != nil {
			continue
		}
		for _, de := range dlEntries {
			dName := de.Name()
			var rawName string
			var ext string
			if strings.HasSuffix(dName, ".apex") {
				rawName = strings.TrimSuffix(dName, ".apex")
				ext = "apex"
			} else if strings.HasSuffix(dName, ".capex") {
				rawName = strings.TrimSuffix(dName, ".capex")
				ext = "capex"
			} else {
				continue
			}

			baseName := rawName
			versionCode := ""
			if idx := strings.LastIndex(rawName, "@"); idx > 0 {
				baseName = rawName[:idx]
				versionCode = rawName[idx+1:]
			}

			if pkg, exists := installedMap[baseName]; exists {
				if ext != "" {
					pkg.pkgType = ext
				}
				if versionCode != "" {
					if pkg.versionCode == "" || compareVersions(versionCode, pkg.versionCode) > 0 {
						pkg.versionCode = versionCode
					}
					if pkg.version == "" {
						pkg.version = versionCode
					}
				}
			}
		}
	}

	// 4. Enrich metadata with DB cache if available
	type dbCand struct {
		arch      string
		microArch string
		apiLevel  string
		version   string
		pkgType   string
	}
	dbPackages := make(map[string][]dbCand)
	for _, cDir := range dbCacheDirs {
		dbFiles, err := filepath.Glob(filepath.Join(cDir, "*.db"))
		if err != nil {
			continue
		}
		for _, dbFile := range dbFiles {
			f, err := os.Open(dbFile)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) >= 6 {
					c := dbCand{
						arch:      parts[1],
						microArch: parts[2],
						apiLevel:  parts[3],
						version:   parts[4],
						pkgType:   "apex",
					}
					if len(parts) >= 7 {
						c.pkgType = parts[6]
					}
					name := parts[0]
					dbPackages[name] = append(dbPackages[name], c)
				}
			}
			f.Close()
		}
	}

	for _, pkg := range installedMap {
		if cands, exists := dbPackages[pkg.name]; exists && len(cands) > 0 {
			var chosen *dbCand
			// First, try matching both version and arch
			for i := range cands {
				c := &cands[i]
				archMatches := pkg.arch == "" || c.arch == "" || c.arch == "any" || c.arch == pkg.arch
				verMatches := (pkg.version != "" && compareVersions(c.version, pkg.version) == 0) ||
					(pkg.versionCode != "" && compareVersions(c.version, pkg.versionCode) == 0)
				if archMatches && verMatches {
					chosen = c
					break
				}
			}
			// If not matched, try matching version only
			if chosen == nil && (pkg.version != "" || pkg.versionCode != "") {
				for i := range cands {
					c := &cands[i]
					verMatches := (pkg.version != "" && compareVersions(c.version, pkg.version) == 0) ||
						(pkg.versionCode != "" && compareVersions(c.version, pkg.versionCode) == 0)
					if verMatches {
						chosen = c
						break
					}
				}
			}
			// If not matched and pkg has no version, try matching arch only
			if chosen == nil && pkg.version == "" {
				for i := range cands {
					c := &cands[i]
					if pkg.arch == "" || c.arch == "" || c.arch == "any" || c.arch == pkg.arch {
						chosen = c
						break
					}
				}
				if chosen == nil {
					chosen = &cands[0]
				}
				pkg.version = chosen.version
			}

			if chosen != nil {
				if pkg.arch == "" && chosen.arch != "any" {
					pkg.arch = chosen.arch
				}
				if pkg.microArch == "" {
					pkg.microArch = chosen.microArch
				}
				if pkg.apiLevel == "" {
					pkg.apiLevel = chosen.apiLevel
				}
				if chosen.pkgType != "" {
					pkg.pkgType = chosen.pkgType
				}
			}
		}
	}

	// Filter packages matching targets (AND logic across targets)
	var matched []*installedPkgInfo
	for _, pkgName := range pkgNames {
		pkg := installedMap[pkgName]

		if !matchInstalledPkgApi(pkg, apiLevelExact, apiLevel) {
			continue
		}

		if len(targets) == 0 {
			matched = append(matched, pkg)
			continue
		}

		matchesAll := true
		for _, t := range targets {
			targetName, targetVersion := parseTarget(t)

			if targetVersion != "" {
				cleanTargetVer := strings.TrimPrefix(targetVersion, "v")
				cleanPkgVer := strings.TrimPrefix(pkg.version, "v")
				cleanPkgCode := strings.TrimPrefix(pkg.versionCode, "v")
				if cleanPkgVer != cleanTargetVer && cleanPkgCode != cleanTargetVer {
					matchesAll = false
					break
				}
			}

			if targetName == "" {
				continue
			}

			lowerTarget := strings.ToLower(targetName)
			matchedTarget := false

			if nameOnly {
				if strings.Contains(strings.ToLower(pkg.name), lowerTarget) {
					matchedTarget = true
				}
			} else if libOnly {
				for _, lib := range pkg.libs {
					if strings.Contains(strings.ToLower(lib), lowerTarget) {
						matchedTarget = true
						break
					}
				}
			} else {
				if strings.Contains(strings.ToLower(pkg.name), lowerTarget) {
					matchedTarget = true
				} else {
					for _, lib := range pkg.libs {
						if strings.Contains(strings.ToLower(lib), lowerTarget) {
							matchedTarget = true
							break
						}
					}
				}
			}

			if !matchedTarget {
				matchesAll = false
				break
			}
		}

		if matchesAll {
			matched = append(matched, pkg)
		}
	}

	if len(matched) == 0 {
		os.Exit(1)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].name < matched[j].name
	})

	for _, pkg := range matched {
		if nameOnly {
			fmt.Println(pkg.name)
		} else {
			fmt.Println(formatLocalPackage(pkg))
		}
	}
	os.Exit(0)
}

// pickProvider shows a pacman-style interactive picker when multiple packages
// provide the same library. Returns the chosen package name.
func pickProvider(targetName string, providerNames []string, repos []*RepoConfig, caches []*RegistryCache) string {
	if len(providerNames) == 1 {
		return providerNames[0]
	}

	fmt.Fprintf(os.Stderr, ":: There are %d providers available for %s:\n", len(providerNames), targetName)

	// Group providers by repo
	type numberedPkg struct {
		num  int
		name string
		repo string
	}
	var numbered []numberedPkg
	idx := 1

	for i, cache := range caches {
		if cache == nil {
			continue
		}
		var repoProviders []string
		for _, pn := range providerNames {
			for _, cand := range cache.Packages {
				if cand.Name == pn {
					repoProviders = append(repoProviders, pn)
					break
				}
			}
		}
		if len(repoProviders) == 0 {
			continue
		}
		repoName := repos[i].Name
		fmt.Fprintf(os.Stderr, ":: Repository %s\n", repoName)
		for _, pn := range repoProviders {
			fmt.Fprintf(os.Stderr, "   %d) %s\n", idx, pn)
			numbered = append(numbered, numberedPkg{idx, pn, repoName})
			idx++
		}
	}

	if NoConfirmMode {
		return numbered[0].name
	}

	fmt.Fprintf(os.Stderr, "\nEnter a number (default=1): ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)

	if line == "" {
		return numbered[0].name
	}

	choice, err := strconv.Atoi(line)
	if err != nil || choice < 1 || choice > len(numbered) {
		fmt.Fprintln(os.Stderr, "Invalid selection, using default.")
		return numbered[0].name
	}

	return numbered[choice-1].name
}

func printUsage() {
	fmt.Print(`apexm - Android APEX Package Manager

Usage:
  apexm <operation> [options] [targets...]

Operations:
  install, -S         Install packages (by package name)
  install, -Sl        Install packages (by library name)
  remove,  -R         Remove packages
  search,  -Ss        Search for packages in repositories
  update,  -Su        Sync/update repository databases
  download,-Sd        Download packages without installing
  local,   -Q         Query locally installed packages for a library
  local,   -Qs        Search locally installed packages
  list                List all installed packages
  init                Re-mount and symlink all installed packages

Target Syntax:
  pkg.name            Install by package name (default with -S)
  libname.so          Install by library name (with -Sl)
  name@versionCode    Pin to a specific versionCode (e.g. libfoo.so@1000)

Options:
`)
	pflag.PrintDefaults()
}

func main() {
	preProcessArgs()

	pflag.CommandLine.SortFlags = false

	configFlag := pflag.StringP("config", "c", "", "Config file path")
	verboseFlag := pflag.BoolP("verbose", "v", false, "Verbose logging")
	
	installMode := pflag.BoolP("install", "S", false, "Install packages")
	removeMode := pflag.BoolP("remove", "R", false, "Remove packages")
	localMode := pflag.BoolP("local", "Q", false, "Query installed packages for a library name (use -Qs to search installed packages)")
	initMode := pflag.Bool("init", false, "Re-mount and symlink all installed APEXes")
	listMode := pflag.Bool("list", false, "List all installed packages")

	isolatedFlag := pflag.BoolP("isolated", "i", false, "Isolated mode: ignore system states (user) or operate on rootdir (system)")
	rootDirFlag := pflag.StringP("rootdir", "r", "", "Root directory for isolated system operations")

	updateFlag := pflag.BoolP("update", "u", false, "Update local repository databases (and override installed)")
	searchFlag := pflag.BoolP("search", "s", false, "Search packages (-Ss for remote repos, -Qs for installed)")
	downloadFlag := pflag.BoolP("download", "d", false, "Download only, do not extract or mount")
	libFlag := pflag.BoolP("library", "l", false, "Treat targets as library names instead of package names")
	noconfirmFlag := pflag.BoolP("noconfirm", "y", false, "Skip interactive prompts, use defaults")

	nameFlag := pflag.BoolP("name", "n", false, "Show only package name or restrict search strictly to package name")
	archFlag := pflag.String("arch", "", "Target architecture (required if --max-microarch is set)")
	maxMicroarch := pflag.String("max-microarch", "", "Highest microarchitecture level to download (prioritizes higher microarch)")
	apiLevel := pflag.Int("api-level", 0, "Highest API level to download (prioritizes higher api-level, min 29)")
	pflag.IntVar(apiLevel, "api", 0, "Highest API level to download (alias for --api-level)")
	apiLevelExact := pflag.Int("api-level-exact", 0, "Filter packages strictly to an exact Android API level (takes precedence over --api-level)")
	pflag.IntVar(apiLevelExact, "api-exact", 0, "Filter packages strictly to an exact Android API level (alias for --api-level-exact)")
	
	pflag.Usage = printUsage
	pflag.Parse()

	modesActive := 0
	if *installMode { modesActive++ }
	if *removeMode { modesActive++ }
	if *localMode { modesActive++ }
	if *initMode { modesActive++ }
	if *listMode { modesActive++ }

	if modesActive > 1 {
		fmt.Println("Error: only one operation may be used at a time")
		os.Exit(1)
	}

	targets := pflag.Args()
	if modesActive == 0 && !*updateFlag && !*searchFlag {
		printUsage()
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
	NoConfirmMode = *noconfirmFlag

	if *maxMicroarch == "" && ActiveConfig.MaxMicroArch != "" {
		*maxMicroarch = ActiveConfig.MaxMicroArch
	}
	if *apiLevel == 0 && ActiveConfig.MaxApiLevel != "" {
		if parsedApi, err := strconv.Atoi(ActiveConfig.MaxApiLevel); err == nil && parsedApi > 0 {
			*apiLevel = parsedApi
		}
	}

	if *initMode {
		handleInit()
		os.Exit(0)
	}

	if *listMode {
		handleList()
		os.Exit(0)
	}

	if *localMode {
		if *searchFlag {
			rootInstall := ""
			rootDownload := ""
			rootCache := ""
			if !isRoot && !*isolatedFlag {
				rootInstall = apexConfig.Root.InstallPath
				rootDownload = apexConfig.Root.DownloadPath
				rootCache = apexConfig.Root.DBCacheDir
			}
			handleLocalSearch(targets, *nameFlag, *libFlag, rootInstall, rootDownload, rootCache, *apiLevelExact, *apiLevel)
			os.Exit(0)
		}

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

		targetName, targetVersionCode := parseTarget(target)

		if IsSatisfiedLib(targetName) {
			continue // skip satisfied system libs
		}
		if resolved[targetName] {
			continue
		}

		var candidates []*PackageCandidate

		useLibLookup := *libFlag || *searchFlag

		// Search across all caches
		for _, cache := range caches {
			if cache == nil {
				continue
			}

			var searchPkgs []string

			if useLibLookup {
				// Library mode: resolve library name to package names via providers
				searchPkgs = []string{targetName}
				for _, sp := range searchPkgs {
					if pkgs, ok := cache.Providers[sp]; ok {
						searchPkgs = append(searchPkgs, pkgs...)
					}
				}
			} else {
				// Package mode: target is a package name directly
				searchPkgs = []string{targetName}
			}

			// Find packages matching the search names
			for _, pkgName := range searchPkgs {
				for _, cand := range cache.Packages {
					if cand.Name == pkgName {
						if *maxMicroarch != "" && parseMicroArch(cand.MicroArch) > parseMicroArch(*maxMicroarch) {
							continue // skip if higher than requested maximum
						}
						if !matchCandidateApi(cand, *apiLevelExact, *apiLevel) {
							continue // skip if not matching requested API level constraints
						}
						candidates = append(candidates, cand)
					}
				}
			}
		}

		if targetVersionCode != "" {
			var filtered []*PackageCandidate
			for _, c := range candidates {
				if c.Version == targetVersionCode {
					filtered = append(filtered, c)
				}
			}
			candidates = filtered
		}

		if len(candidates) == 0 {
			fmt.Printf("Error: Unresolvable dependency: %s\n", targetName)
			os.Exit(1)
		}

		sortCandidates(candidates, *maxMicroarch)

		if *searchFlag {
			for _, c := range candidates {
				ext := c.Type
				if ext == "" {
					ext = "apex"
				}
				microarchStr := c.MicroArch
				if !strings.HasPrefix(microarchStr, "v") {
					microarchStr = "v" + microarchStr
				}
				fmt.Printf("%s/%s %s (%s, %s-%s, API %s)\n",
					c.Repo.Name, c.Name, c.Version, ext, c.Arch, microarchStr, c.ApiLevel)
			}
			resolved[targetName] = true
			continue // do not add to installList and don't resolve dependencies
		}

		// When using -l with multiple providers, show a picker
		if *libFlag {
			// Collect unique provider package names
			providerSet := make(map[string]bool)
			var providerNames []string
			for _, c := range candidates {
				if !providerSet[c.Name] {
					providerSet[c.Name] = true
					providerNames = append(providerNames, c.Name)
				}
			}

			if len(providerNames) > 1 {
				chosen := pickProvider(targetName, providerNames, repos, caches)
				// Filter candidates to only the chosen package
				var filtered []*PackageCandidate
				for _, c := range candidates {
					if c.Name == chosen {
						filtered = append(filtered, c)
					}
				}
				candidates = filtered
				sortCandidates(candidates, *maxMicroarch)
			}
		}

		selected := candidates[0]

		ext := selected.Type
		if ext == "" {
			ext = "apex"
		}
		LogV("Resolved %s -> %s.%s v%s (Repo: %s)", targetName, selected.Name, ext, selected.Version, selected.Repo.Name)

		if resolved[selected.Name] {
			resolved[targetName] = true
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
		resolved[targetName] = true
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
