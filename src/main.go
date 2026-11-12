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
		"--api-level", "--api", "--api-level-exact", "--api-exact",
		"--skip-abi-level", "--skip-abi-custom":
		return true
	}
	return false
}

func isOpFlag(arg string) bool {
	if arg == "--install" || arg == "--remove" || arg == "--local" || arg == "--init" || arg == "--list" || arg == "--files" {
		return true
	}
	if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
		for _, r := range arg[1:] {
			if r == 'S' || r == 'R' || r == 'Q' || r == 'F' {
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
			case "search", "install", "remove", "init", "update", "download", "local", "list", "files":
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
			case "files":
				os.Args[subcmdIdx] = "-F"
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

type localQueryOptions struct {
	installPaths []string
	mergePaths   []string
	dbCacheDirs  []string
}

type installedDirEntry struct {
	pkgName string
	dir     string
}

func appendUniqueExpandedPath(list *[]string, seen map[string]bool, raw string) {
	if raw == "" {
		return
	}
	p := filepath.Clean(expandTilde(raw))
	if !seen[p] {
		seen[p] = true
		*list = append(*list, p)
	}
}

func collectLocalQueryOptions(apexConfig *ApexConfig, isRoot, isolated bool, rootDir string) localQueryOptions {
	var opts localQueryOptions
	seenInstall := make(map[string]bool)
	seenMerge := make(map[string]bool)
	seenCache := make(map[string]bool)

	if envSearch := os.Getenv("APEX_SEARCH_PATH"); envSearch != "" {
		for _, p := range filepath.SplitList(envSearch) {
			appendUniqueExpandedPath(&opts.installPaths, seenInstall, p)
			appendUniqueExpandedPath(&opts.mergePaths, seenMerge, p)
		}
	}

	if rootDir != "" {
		appendUniqueExpandedPath(&opts.installPaths, seenInstall, filepath.Join(rootDir, "apex"))
		appendUniqueExpandedPath(&opts.mergePaths, seenMerge, filepath.Join(rootDir, "apex"))
		appendUniqueExpandedPath(&opts.dbCacheDirs, seenCache, filepath.Join(rootDir, "var", "cache", "apex", "sync"))
	}

	appendUniqueExpandedPath(&opts.installPaths, seenInstall, ActiveConfig.InstallPath)
	appendUniqueExpandedPath(&opts.mergePaths, seenMerge, ActiveConfig.MergePath)
	appendUniqueExpandedPath(&opts.dbCacheDirs, seenCache, ActiveConfig.DBCacheDir)

	if !isRoot && !isolated && apexConfig != nil {
		appendUniqueExpandedPath(&opts.installPaths, seenInstall, apexConfig.Root.InstallPath)
		appendUniqueExpandedPath(&opts.mergePaths, seenMerge, apexConfig.Root.MergePath)
		appendUniqueExpandedPath(&opts.dbCacheDirs, seenCache, apexConfig.Root.DBCacheDir)
	}

	return opts
}

func isMergedSystemSubdir(name string) bool {
	switch name {
	case "lib", "lib64", "libx32", "bin", "sbin", "share", "include", "etc", "usr", "var", "opt":
		return true
	}
	return false
}

func hasApexManifest(pkgDir string) bool {
	for _, name := range []string{"apex_manifest.json", "apex_manifest.pb", ".APEXINFO"} {
		if _, err := os.Stat(filepath.Join(pkgDir, name)); err == nil {
			return true
		}
	}
	return false
}

func listInstalledPackageDirs(installPaths []string) []installedDirEntry {
	var result []installedDirEntry
	seen := make(map[string]bool)

	for _, root := range installPaths {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			pkgName := entry.Name()
			if strings.HasPrefix(pkgName, ".") || seen[pkgName] {
				continue
			}
			pkgDir := filepath.Join(root, pkgName)
			info, err := os.Stat(pkgDir)
			if err != nil || !info.IsDir() {
				continue
			}
			if isMergedSystemSubdir(pkgName) && !hasApexManifest(pkgDir) {
				continue
			}
			sub, err := os.ReadDir(pkgDir)
			if err != nil || len(sub) == 0 {
				continue
			}
			seen[pkgName] = true
			result = append(result, installedDirEntry{
				pkgName: pkgName,
				dir:     pkgDir,
			})
		}
	}

	return result
}

func stripLocalVersionConstraint(target string) string {
	if idx := strings.IndexAny(target, "><="); idx >= 0 {
		target = target[:idx]
	}
	if idx := strings.LastIndex(target, "@"); idx > 0 {
		target = target[:idx]
	}
	return strings.TrimSpace(target)
}

func fileExistsRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func findRelativeFile(baseDir string, candidates []string) (string, bool) {
	for _, rel := range candidates {
		p := filepath.Join(baseDir, rel)
		if fileExistsRegular(p) {
			return p, true
		}
	}
	return "", false
}

func walkFindFirst(rootDir string, maxDepth int, match func(path string, d os.DirEntry) bool) (string, bool) {
	if _, err := os.Stat(rootDir); err != nil {
		return "", false
	}
	baseDepth := strings.Count(filepath.Clean(rootDir), string(os.PathSeparator))
	var found string
	stopErr := fmt.Errorf("found")
	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if path != rootDir {
			depth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - baseDepth
			if d.IsDir() && maxDepth > 0 && depth > maxDepth {
				return filepath.SkipDir
			}
		}
		if match(path, d) {
			found = path
			return stopErr
		}
		return nil
	})
	return found, found != ""
}

func matchPythonSitePackages(pkgDir, pyVer, mod string) (string, bool) {
	for _, base := range []string{"lib", "lib64", "libx32", "share"} {
		spDir := filepath.Join(pkgDir, base, "python"+pyVer, "site-packages")
		entries, err := os.ReadDir(spDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			switch {
			case name == mod+".py" && !e.IsDir():
				return filepath.Join(spDir, name), true
			case name == mod && e.IsDir():
				return filepath.Join(spDir, name), true
			case (strings.HasSuffix(name, ".dist-info") || strings.HasSuffix(name, ".egg-info")) &&
				(strings.HasPrefix(name, mod+"-") || strings.HasPrefix(name, mod+".")):
				return filepath.Join(spDir, name), true
			}
		}
	}
	return "", false
}

func matchInstalledSoname(pkgDir, target string, substring bool) (string, bool) {
	libSubdirs := []string{"lib64", "libx32", "lib", filepath.Join("lib64", "bionic"), filepath.Join("lib", "bionic")}
	for _, libDir := range libSubdirs {
		dirPath := filepath.Join(pkgDir, libDir)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if (!substring && name == target) || (substring && strings.Contains(name, target)) {
				return filepath.Join(dirPath, name), true
			}
		}
	}
	return "", false
}

func matchInstalledCapability(pkg installedDirEntry, tag, val string) (string, bool) {
	switch tag {
	case "pc":
		targetFile := val + ".pc"
		if p, ok := findRelativeFile(pkg.dir, []string{
			filepath.Join("lib64", "pkgconfig", targetFile),
			filepath.Join("libx32", "pkgconfig", targetFile),
			filepath.Join("lib", "pkgconfig", targetFile),
			filepath.Join("lib", "pkg-config", targetFile),
			filepath.Join("share", "pkgconfig", targetFile),
		}); ok {
			return p, true
		}
		return walkFindFirst(pkg.dir, 4, func(_ string, d os.DirEntry) bool {
			return !d.IsDir() && d.Name() == targetFile
		})
	case "cmake":
		names := []string{val + "Config.cmake", val + "-config.cmake"}
		for _, base := range []string{"lib64", "libx32", "lib", "share"} {
			for _, sub := range []string{
				filepath.Join(pkg.dir, base, "cmake", val),
				filepath.Join(pkg.dir, base, "cmake", strings.ToLower(val)),
				filepath.Join(pkg.dir, base, "cmake"),
			} {
				if p, ok := findRelativeFile(sub, names); ok {
					return p, true
				}
			}
		}
		return walkFindFirst(pkg.dir, 5, func(_ string, d os.DirEntry) bool {
			if d.IsDir() {
				return false
			}
			return d.Name() == names[0] || d.Name() == names[1]
		})
	case "bin":
		p := filepath.Join(pkg.dir, "bin", val)
		if fileExistsRegular(p) {
			return p, true
		}
	case "header":
		if p, ok := findRelativeFile(filepath.Join(pkg.dir, "include"), []string{val, val + ".h", val + ".hpp"}); ok {
			return p, true
		}
		if pkg.pkgName == val || deriveShortAlias(pkg.pkgName) == val {
			if _, ok := walkFindFirst(filepath.Join(pkg.dir, "include"), 4, func(_ string, d os.DirEntry) bool {
				return !d.IsDir() && (strings.HasSuffix(d.Name(), ".h") || strings.HasSuffix(d.Name(), ".hpp"))
			}); ok {
				return filepath.Join(pkg.dir, "include"), true
			}
		}
	case "jar":
		return findRelativeFile(pkg.dir, []string{
			filepath.Join("share", "java", val+".jar"),
			filepath.Join("javalib", val+".jar"),
		})
	case "apk":
		for _, sub := range []string{"app", "priv-app"} {
			if p, ok := walkFindFirst(filepath.Join(pkg.dir, sub), 3, func(_ string, d os.DirEntry) bool {
				return !d.IsDir() && d.Name() == val+".apk"
			}); ok {
				return p, true
			}
		}
	case "font":
		for _, sub := range []string{"fonts", filepath.Join("etc", "fonts"), filepath.Join("share", "fonts")} {
			if p, ok := walkFindFirst(filepath.Join(pkg.dir, sub), 4, func(_ string, d os.DirEntry) bool {
				return !d.IsDir() && (d.Name() == val+".ttf" || d.Name() == val+".otf")
			}); ok {
				return p, true
			}
		}
	case "completion":
		for _, sub := range []string{filepath.Join("share", "bash-completion"), filepath.Join("share", "zsh"), filepath.Join("share", "fish")} {
			if p, ok := walkFindFirst(filepath.Join(pkg.dir, sub), 4, func(_ string, d os.DirEntry) bool {
				return !d.IsDir() && d.Name() == val
			}); ok {
				return p, true
			}
		}
	case "alias":
		if pkg.pkgName == val || deriveShortAlias(pkg.pkgName) == val {
			return pkg.dir, true
		}
	default:
		if strings.HasPrefix(tag, "py") {
			pyVer := strings.TrimPrefix(tag, "py")
			return matchPythonSitePackages(pkg.dir, pyVer, val)
		}
		if strings.HasSuffix(val, ".so") || strings.Contains(val, ".so.") {
			return matchInstalledSoname(pkg.dir, val, false)
		}
	}
	return "", false
}

func matchMergedPkgConfig(pcName string, mergePaths []string, installed []installedDirEntry) (string, string, bool) {
	targetFile := pcName + ".pc"
	relPaths := []string{
		filepath.Join("lib64", "pkgconfig", targetFile),
		filepath.Join("libx32", "pkgconfig", targetFile),
		filepath.Join("lib", "pkgconfig", targetFile),
		filepath.Join("lib", "pkg-config", targetFile),
		filepath.Join("share", "pkgconfig", targetFile),
	}
	for _, mp := range mergePaths {
		if p, ok := findRelativeFile(mp, relPaths); ok {
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				continue
			}
			for _, pkg := range installed {
				cleanDir := filepath.Clean(pkg.dir)
				if resolved == cleanDir || strings.HasPrefix(resolved, cleanDir+string(os.PathSeparator)) {
					return pkg.pkgName, p, true
				}
			}
		}
	}
	return "", "", false
}

func lookupInstalledViaProviders(query string, installedMap map[string]string, dbCacheDirs []string) (string, string, bool) {
	for _, cacheDir := range dbCacheDirs {
		provFiles, err := filepath.Glob(filepath.Join(cacheDir, "*.providers"))
		if err != nil {
			continue
		}
		sort.Strings(provFiles)
		for _, pf := range provFiles {
			f, err := os.Open(pf)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				parts := strings.Fields(strings.TrimSpace(scanner.Text()))
				if len(parts) < 6 {
					continue
				}
				if parts[0] == query {
					pkgName := parts[4]
					if _, isInstalled := installedMap[pkgName]; isInstalled {
						f.Close()
						return pkgName, pf, true
					}
				}
			}
			f.Close()
		}
	}
	return "", "", false
}

func resolveLocalTarget(rawTarget string, opts localQueryOptions) (string, string, bool) {
	target := stripLocalVersionConstraint(rawTarget)
	if target == "" {
		return "", "", false
	}

	installed := listInstalledPackageDirs(opts.installPaths)
	if len(installed) == 0 {
		return "", "", false
	}

	installedMap := make(map[string]string, len(installed))
	for _, pkg := range installed {
		installedMap[pkg.pkgName] = pkg.dir
	}

	if idx := strings.Index(target, ":"); idx > 0 {
		tag, val := target[:idx], target[idx+1:]
		for _, pkg := range installed {
			if loc, ok := matchInstalledCapability(pkg, tag, val); ok {
				return pkg.pkgName, loc, true
			}
		}
		if tag == "pc" {
			if pkgName, loc, ok := matchMergedPkgConfig(val, opts.mergePaths, installed); ok {
				return pkgName, loc, true
			}
		}
		if pkgName, loc, ok := lookupInstalledViaProviders(target, installedMap, opts.dbCacheDirs); ok {
			return pkgName, loc, true
		}
		return "", "", false
	}

	if dir, ok := installedMap[target]; ok {
		return target, dir, true
	}

	for _, pkg := range installed {
		if loc, ok := matchInstalledSoname(pkg.dir, target, false); ok {
			return pkg.pkgName, loc, true
		}
	}

	for _, pkg := range installed {
		if deriveShortAlias(pkg.pkgName) == target {
			return pkg.pkgName, pkg.dir, true
		}
	}

	if pkgName, loc, ok := lookupInstalledViaProviders(target, installedMap, opts.dbCacheDirs); ok {
		return pkgName, loc, true
	}

	for _, pkg := range installed {
		if loc, ok := matchInstalledSoname(pkg.dir, target, true); ok {
			return pkg.pkgName, loc, true
		}
	}

	return "", "", false
}

func handleLocal(libName string, nameOnly bool, opts localQueryOptions) {
	pkgName, loc, ok := resolveLocalTarget(libName, opts)
	if !ok {
		os.Exit(1)
	}
	if nameOnly {
		fmt.Println(pkgName)
	} else {
		fmt.Printf("Found in: %s (at %s)\n", pkgName, loc)
	}
	os.Exit(0)
}

func handleList(opts localQueryOptions) {
	pkgs := listInstalledPackageDirs(opts.installPaths)
	for _, pkg := range pkgs {
		fmt.Println(pkg.pkgName)
	}
	if len(pkgs) == 0 {
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

func handleLocalSearch(targets []string, nameOnly, libOnly bool, opts localQueryOptions, rootDownload string, apiLevelExact, apiLevel int) {
	installPaths := opts.installPaths
	downloadPaths := []string{ActiveConfig.DownloadPath}
	if rootDownload != "" && expandTilde(rootDownload) != ActiveConfig.DownloadPath {
		if _, err := os.Stat(expandTilde(rootDownload)); err == nil {
			downloadPaths = append(downloadPaths, expandTilde(rootDownload))
		}
	}
	dbCacheDirs := opts.dbCacheDirs

	installedMap := make(map[string]*installedPkgInfo)
	var pkgNames []string

	for _, entry := range listInstalledPackageDirs(installPaths) {
		info := &installedPkgInfo{
			name:       entry.pkgName,
			installDir: entry.dir,
			pkgType:    "apex",
		}
		installedMap[entry.pkgName] = info
		pkgNames = append(pkgNames, entry.pkgName)
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
		if pkg.version == "" {
			pbPaths := []string{
				filepath.Join(pkg.installDir, "apex_manifest.pb"),
			}
			for _, mpb := range pbPaths {
				if data, err := os.ReadFile(mpb); err == nil {
					_, vCode, vName := parseApexManifestPb(data)
					if vName != "" {
						pkg.version = vName
					}
					if vCode != "" {
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
  files,   -F         Query the file database (<repo>.files.zst) for packages owning files
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

func parseApexManifestPb(data []byte) (name string, versionCode string, versionName string) {
	idx := 0
	for idx < len(data) {
		tag := data[idx]
		idx++
		fieldNum := tag >> 3
		wireType := tag & 0x07
		switch wireType {
		case 0: // varint
			var val uint64
			var shift uint
			for idx < len(data) {
				b := data[idx]
				idx++
				val |= uint64(b&0x7F) << shift
				if b&0x80 == 0 {
					break
				}
				shift += 7
			}
			if fieldNum == 2 {
				versionCode = strconv.FormatUint(val, 10)
			}
		case 2: // length-delimited string
			var length int
			var shift uint
			for idx < len(data) {
				b := data[idx]
				idx++
				length |= int(b&0x7F) << shift
				if b&0x80 == 0 {
					break
				}
				shift += 7
			}
			if idx+length <= len(data) {
				strVal := string(data[idx : idx+length])
				idx += length
				if fieldNum == 1 {
					name = strVal
				} else if fieldNum == 3 {
					versionName = strVal
				}
			}
		default:
			return
		}
	}
	return
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
	filesSearchFlag := pflag.BoolP("files", "F", false, "Query the file database (<repo>.files.zst) for packages owning files")

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
	skipAbiLevel := pflag.Int("skip-abi-level", 2, "Skip ABI level (0: none, 1: bionic libc, 2: all NDK [default], 3: NDK + VNDK)")
	disableSkipAbis := pflag.Bool("disable-skip-abis", false, "Alias for --skip-abi-level 0 (do not skip any ABIs)")
	skipAbiCustom := pflag.StringSlice("skip-abi-custom", nil, "Custom libraries to skip (path, lib:path, or lib name, comma-separated or repeated)")
	
	pflag.Usage = printUsage
	pflag.Parse()

	modesActive := 0
	if *installMode { modesActive++ }
	if *removeMode { modesActive++ }
	if *localMode { modesActive++ }
	if *initMode { modesActive++ }
	if *listMode { modesActive++ }
	if *filesSearchFlag { modesActive++ }

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
		localOpts := collectLocalQueryOptions(apexConfig, isRoot, *isolatedFlag, *rootDirFlag)
		handleList(localOpts)
		os.Exit(0)
	}

	if *localMode {
		localOpts := collectLocalQueryOptions(apexConfig, isRoot, *isolatedFlag, *rootDirFlag)
		if *searchFlag {
			rootDownload := ""
			if !isRoot && !*isolatedFlag {
				rootDownload = apexConfig.Root.DownloadPath
			}
			handleLocalSearch(targets, *nameFlag, *libFlag, localOpts, rootDownload, *apiLevelExact, *apiLevel)
			os.Exit(0)
		}

		if len(targets) == 0 {
			fmt.Println("Error: no library name specified for local query")
			os.Exit(1)
		}
		handleLocal(targets[0], *nameFlag, localOpts)
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
		if len(targets) == 0 && !*filesSearchFlag {
			os.Exit(0)
		}
	}

	if *filesSearchFlag {
		if len(targets) == 0 {
			fmt.Println("Error: no file path or pattern specified for -F/--files")
			os.Exit(1)
		}
		for _, t := range targets {
			searchFiles(t, repos)
		}
		os.Exit(0)
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

	effectiveSkipAbiLevel := *skipAbiLevel
	if *disableSkipAbis && !pflag.CommandLine.Changed("skip-abi-level") {
		effectiveSkipAbiLevel = 0
	}
	customSkippedMap := parseCustomSkipAbis(*skipAbiCustom)
	effectiveApiLevel := 0
	if *apiLevelExact > 0 {
		effectiveApiLevel = *apiLevelExact
	} else if *apiLevel > 0 {
		effectiveApiLevel = *apiLevel
	}

	queue := append([]string{}, targets...)
	resolved := make(map[string]bool)
	var installList []*PackageCandidate

	for len(queue) > 0 {
		target := queue[0]
		queue = queue[1:]

		targetName, targetVersionCode := parseTarget(target)

		if IsSatisfiedLibLevel(targetName, effectiveSkipAbiLevel, effectiveApiLevel, customSkippedMap) {
			continue // skip satisfied system libs
		}
		if resolved[targetName] {
			continue
		}

		var candidates []*PackageCandidate

		useLibLookup := *libFlag || *searchFlag || strings.Contains(targetName, ":") || strings.HasSuffix(targetName, ".so")

		// Search across all caches
		for _, cache := range caches {
			if cache == nil {
				continue
			}

			var searchPkgs []string

			if useLibLookup {
				// Capability/Library mode: resolve capability/library name to package names via providers
				searchPkgs = []string{targetName}
				if pkgs, ok := cache.Providers[targetName]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
				if pkgs, ok := cache.Providers["alias:"+targetName]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
				if pkgs, ok := cache.ShortAliases[targetName]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
				if *searchFlag {
					// Also search inside Providers for keys containing targetName
					for provKey, provPkgs := range cache.Providers {
						if strings.Contains(provKey, targetName) {
							searchPkgs = append(searchPkgs, provPkgs...)
						}
					}
					// Also search package names containing targetName
					for _, cand := range cache.Packages {
						if strings.Contains(cand.Name, targetName) {
							searchPkgs = append(searchPkgs, cand.Name)
						}
					}
				}
			} else {
				// Package mode: target is a package name directly, but also check aliases
				searchPkgs = []string{targetName}
				if pkgs, ok := cache.Providers["alias:"+targetName]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
				if pkgs, ok := cache.ShortAliases[targetName]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
			}

			// Find packages matching the search names
			seenSearch := make(map[string]bool)
			for _, pkgName := range searchPkgs {
				if seenSearch[pkgName] {
					continue
				}
				seenSearch[pkgName] = true
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

		// When multiple packages provide the target, show an interactive picker
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
