package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

var satisfiedLibs = map[string]bool{
	"libaaudio.so": true, "libamidi.so": true, "libandroid.so": true, "libbinder_ndk.so": true,
	"libcamera2ndk.so": true, "libc++.so": true, "libc.so": true, "libdl.so": true, "libEGL.so": true,
	"libGLESv1_CM.so": true, "libGLESv2.so": true, "libGLESv3.so": true, "libicu.so": true,
	"libjnigraphics.so": true, "liblog.so": true, "libmediandk.so": true, "libm.so": true,
	"libnativehelper.so": true, "libnativewindow.so": true, "libneuralnetworks.so": true,
	"libOpenMAXAL.so": true, "libOpenSLES.so": true, "libstdc++.so": true, "libsync.so": true,
	"libvulkan.so": true, "libz.so": true, "libc++_shared.so": true, "libcutils.so": true,
	"libhardware.so": true,
}

type RepoConfig struct {
	Name      string
	URL       string
	AuthToken string
	AuthBasic string
	Arch      string
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
		}
	}
	return repos, scanner.Err()
}

func doReq(cfg *RepoConfig, urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	} else if cfg.AuthBasic != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.AuthBasic))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	client := &http.Client{}
	return client.Do(req)
}

type PackageCandidate struct {
	RepoPriority int
	Repo         *RepoConfig
	Name         string
	Version      string
	Org          string
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

func fetchRepoData(repoIndex int, repo *RepoConfig) (*RegistryCache, error) {
	cache := &RegistryCache{
		Providers: make(map[string][]string),
	}

	syncDir := "/var/cache/apex/sync"
	if _, err := os.Stat(syncDir); os.IsNotExist(err) {
		syncDir = filepath.Join(os.Getenv("HOME"), ".cache/apex/sync")
	}

	// 1. Fetch Providers
	provFile := filepath.Join(syncDir, repo.Name+".providers.tar.gz")
	var provReader io.ReadCloser
	if f, err := os.Open(provFile); err == nil {
		provReader = f
	}

	if provReader != nil {
		gzr, err := gzip.NewReader(provReader)
		if err == nil {
			tr := tar.NewReader(gzr)
			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				if header.Name == "providers" || header.Name == repo.Name+".providers" {
					scanner := bufio.NewScanner(tr)
					for scanner.Scan() {
						line := strings.TrimSpace(scanner.Text())
						parts := strings.SplitN(line, ":", 2)
						if len(parts) == 2 {
							lib := strings.TrimSpace(parts[0])
							pkgs := strings.Fields(strings.TrimSpace(parts[1]))
							cache.Providers[lib] = append(cache.Providers[lib], pkgs...)
						}
					}
				}
			}
			gzr.Close()
		}
		provReader.Close()
	}

	// 2. Fetch DB
	dbFile := filepath.Join(syncDir, repo.Name+".db.tar.gz")
	var dbReader io.ReadCloser
	if f, err := os.Open(dbFile); err == nil {
		dbReader = f
	} else {
		return nil, fmt.Errorf("repository database missing locally, run with --update first")
	}
	defer dbReader.Close()

	gzr, err := gzip.NewReader(dbReader)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(header.Name, "/desc") {
			content, err := io.ReadAll(tr)
			if err != nil {
				continue
			}
			lines := strings.Split(string(content), "\n")

			cand := &PackageCandidate{
				RepoPriority: repoIndex,
				Repo:         repo,
			}

			var currentSection string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
					currentSection = line
					continue
				}
				if line == "" {
					currentSection = ""
					continue
				}

				switch currentSection {
				case "%NAME%":
					cand.Name = line
				case "%VERSION%":
					cand.Version = line
				case "%ARCH%":
					cand.Arch = line
				case "%MICROARCH%":
					cand.MicroArch = line
				case "%APILEVEL%":
					cand.ApiLevel = line
				case "%ORG%":
					cand.Org = line
				case "%DEPENDS%":
					cand.Depends = append(cand.Depends, line)
				case "%PROVIDES%":
					cand.Provides = append(cand.Provides, line)
				}
			}

			if repo.Arch == "" || repo.Arch == cand.Arch || cand.Arch == "any" {
				cache.Packages = append(cache.Packages, cand)
			}
		}
	}

	return cache, nil
}

func sortCandidates(candidates []*PackageCandidate, maxMicroarch string) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RepoPriority != candidates[j].RepoPriority {
			return candidates[i].RepoPriority < candidates[j].RepoPriority
		}
		if candidates[i].MicroArch != candidates[j].MicroArch {
			if maxMicroarch != "" {
				return candidates[i].MicroArch > candidates[j].MicroArch // highest first
			}
			return candidates[i].MicroArch < candidates[j].MicroArch // lowest first
		}
		
		apiI, _ := strconv.Atoi(candidates[i].ApiLevel)
		apiJ, _ := strconv.Atoi(candidates[j].ApiLevel)
		if apiI == 0 { apiI = 29 }
		if apiJ == 0 { apiJ = 29 }
		if apiI != apiJ {
			return apiI > apiJ
		}

		v1 := candidates[i].Version
		v2 := candidates[j].Version
		if !strings.HasPrefix(v1, "v") {
			v1 = "v" + v1
		}
		if !strings.HasPrefix(v2, "v") {
			v2 = "v" + v2
		}
		return semver.Compare(v1, v2) > 0 // highest version first
	})
}

func resolveExtension(cand *PackageCandidate) string {
	dirMicroArch := strings.ReplaceAll(cand.MicroArch, "_", ".")
	archSegment := cand.Arch
	if dirMicroArch != "" {
		archSegment = fmt.Sprintf("%s/v%s", cand.Arch, dirMicroArch)
	}

	apiLevelSegment := cand.ApiLevel
	if apiLevelSegment == "" || apiLevelSegment == "0" {
		apiLevelSegment = "29"
	}

	orgPath := strings.ReplaceAll(cand.Org, ".", "/")
	pkgFilename := fmt.Sprintf("%s-%s.capex", cand.Name, cand.Version)
	urlStr := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)

	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return "apex"
	}
	if cand.Repo.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cand.Repo.AuthToken)
	} else if cand.Repo.AuthBasic != "" {
		req.Header.Set("Authorization", "Basic "+cand.Repo.AuthBasic)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return "capex"
		}
	}
	return "apex"
}

func downloadAndExtract(cand *PackageCandidate) error {
	dirMicroArch := strings.ReplaceAll(cand.MicroArch, "_", ".")
	archSegment := cand.Arch
	if dirMicroArch != "" {
		archSegment = fmt.Sprintf("%s/v%s", cand.Arch, dirMicroArch)
	}

	apiLevelSegment := cand.ApiLevel
	if apiLevelSegment == "" || apiLevelSegment == "0" {
		apiLevelSegment = "29"
	}

	orgPath := strings.ReplaceAll(cand.Org, ".", "/")
	pkgFilename := fmt.Sprintf("%s-%s.capex", cand.Name, cand.Version)
	urlStr := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)

	fmt.Printf("Downloading %s...\n", urlStr)
	resp, err := doReq(cand.Repo, urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		pkgFilename = fmt.Sprintf("%s-%s.apex", cand.Name, cand.Version)
		urlStr = fmt.Sprintf("%s/%s/%s/%s", cand.Repo.URL, archSegment, orgPath, pkgFilename)
		resp2, err := doReq(cand.Repo, urlStr)
		if err != nil {
			return err
		}
		defer resp2.Body.Close()
		resp = resp2
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download package: %s", resp.Status)
	}

	targetDir := fmt.Sprintf("/opt/apex/%s.%s.apex", cand.Org, cand.Name)
	os.MkdirAll(targetDir, 0755)

	tmpFile := filepath.Join(targetDir, "pkg.zip")
	out, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		return err
	}

	zr, err := zip.OpenReader(tmpFile)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		path := filepath.Join(targetDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(path, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0755)
		outFile, err := os.Create(path)
		if err != nil {
			continue
		}
		rc, err := f.Open()
		if err == nil {
			io.Copy(outFile, rc)
			rc.Close()
		}
		outFile.Close()
	}
	zr.Close()
	os.Remove(tmpFile)

	payloadImg := filepath.Join(targetDir, "apex_payload.img")
	if _, err := os.Stat(payloadImg); err != nil {
		return fmt.Errorf("apex_payload.img not found")
	}

	cmd := exec.Command("losetup", "-f", "--show", payloadImg)
	cmdOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("losetup failed: %v", err)
	}
	loopDev := strings.TrimSpace(string(cmdOut))

	mountPoint := fmt.Sprintf("/apex/%s.%s", cand.Org, cand.Name)
	os.MkdirAll(mountPoint, 0755)

	fmt.Printf("Mounting %s to %s...\n", loopDev, mountPoint)
	cmd = exec.Command("mount", "-t", "auto", "-o", "ro", loopDev, mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to mount %s to %s: %v\nOutput: %s", loopDev, mountPoint, err, string(output))
	}

	createSymlinks(fmt.Sprintf("%s.%s", cand.Org, cand.Name))

	return nil
}

func linkContents(srcDir, destDir string) {
	if stat, err := os.Stat(srcDir); err != nil || !stat.IsDir() {
		return
	}
	os.MkdirAll(destDir, 0755)
	entries, _ := os.ReadDir(srcDir)
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		destPath := filepath.Join(destDir, entry.Name())

		if entry.IsDir() {
			linkContents(srcPath, destPath)
		} else {
			os.Remove(destPath)
			os.Symlink(srcPath, destPath)
		}
	}
}

func createSymlinks(pkgName string) {
	mountPoint := filepath.Join("/apex", pkgName)

	// Process include
	includesSrc := filepath.Join(mountPoint, "includes")
	if _, err := os.Stat(includesSrc); os.IsNotExist(err) {
		includesSrc = filepath.Join(mountPoint, "include") // Fallback
	}
	linkContents(includesSrc, "/apex/include")

	// Process lib
	linkContents(filepath.Join(mountPoint, "lib"), "/apex/lib")
	
	// If lib/pkg-config exists, also merge it into the standard /apex/lib/pkgconfig
	pkgConfigDash := filepath.Join(mountPoint, "lib", "pkg-config")
	if stat, err := os.Stat(pkgConfigDash); err == nil && stat.IsDir() {
		linkContents(pkgConfigDash, "/apex/lib/pkgconfig")
	}

	// Process bin
	linkContents(filepath.Join(mountPoint, "bin"), "/apex/bin")

	// Process share
	linkContents(filepath.Join(mountPoint, "share"), "/apex/share")
}

func getMicroarchFallbacks(libPrefix, arch, maxMicroarch string) []string {
	var fallbacks []string
	if arch == "x86_64" {
		if len(maxMicroarch) >= 2 && maxMicroarch[0] == 'v' {
			lvl := int(maxMicroarch[1] - '0')
			for i := lvl; i >= 2; i-- {
				fallbacks = append(fallbacks, fmt.Sprintf("%s-%s-v%d.so", libPrefix, arch, i))
			}
		}
	} else if arch == "aarch64" {
		if len(maxMicroarch) >= 4 && strings.HasPrefix(maxMicroarch, "v8_") {
			lvl := int(maxMicroarch[3] - '0')
			for i := lvl; i >= 1; i-- {
				fallbacks = append(fallbacks, fmt.Sprintf("%s-%s-v8_%d.so", libPrefix, arch, i))
			}
		}
	}
	return fallbacks
}

func tryDownload(repo *RepoConfig, syncDir, suffix string, fallbacks []string) error {
	for _, s := range fallbacks {
		urlStr := fmt.Sprintf("%s/%s%s", strings.TrimRight(repo.URL, "/"), repo.Name, s)
		resp, err := doReq(repo, urlStr)
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusOK {
			dest := filepath.Join(syncDir, repo.Name+suffix)
			f, err := os.Create(dest)
			if err != nil {
				resp.Body.Close()
				return err
			}
			io.Copy(f, resp.Body)
			f.Close()
			resp.Body.Close()
			return nil
		}
		resp.Body.Close()
	}
	return fmt.Errorf("could not download %s (404 Not Found)", suffix)
}

func doUpdate(repos []*RepoConfig) {
	syncDir := "/var/cache/apex/sync"
	err := os.MkdirAll(syncDir, 0755)
	if err != nil {
		syncDir = filepath.Join(os.Getenv("HOME"), ".cache/apex/sync")
		os.MkdirAll(syncDir, 0755)
	}
	for _, repo := range repos {
		fmt.Printf("Updating repo %s...\n", repo.Name)
		errProv := tryDownload(repo, syncDir, ".providers.tar.gz", []string{".providers", ".providers.tar.gz"})
		if errProv != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", errProv)
		}
		errDb := tryDownload(repo, syncDir, ".db.tar.gz", []string{".db", ".db.tar.gz"})
		if errDb != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", errDb)
		}
	}
}

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

		if satisfiedLibs[target] {
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
			
			// Auto-append fallback variants if it's a library and architecture/microarch is defined
			libPrefix := target
			if strings.HasSuffix(target, ".so") {
				libPrefix = strings.TrimSuffix(target, ".so")
			}
			
			if strings.HasSuffix(target, ".so") && cache.Packages != nil && len(cache.Packages) > 0 {
				if *maxMicroarch != "" && *archFlag != "" {
					fallbacks := getMicroarchFallbacks(libPrefix, *archFlag, *maxMicroarch)
					searchPkgs = append(searchPkgs, fallbacks...)
				}
			}

			for _, sp := range searchPkgs {
				if pkgs, ok := cache.Providers[sp]; ok {
					searchPkgs = append(searchPkgs, pkgs...)
				}
			}

			// Find packages matching the search names
			for _, pkgName := range searchPkgs {
				for _, cand := range cache.Packages {
					if cand.Name == pkgName {
						if *maxMicroarch != "" && cand.MicroArch > *maxMicroarch {
							continue // skip if higher than requested maximum
						}
						if *apiLevel > 0 {
							cApi, _ := strconv.Atoi(cand.ApiLevel)
							if cApi == 0 { cApi = 29 }
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
			fmt.Printf("%s.%s.%s %s %s-%s\n", selected.Org, selected.Name, ext, selected.Version, selected.Arch, microarchStr)
			continue // do not add to installList and don't resolve dependencies
		}

		fmt.Printf("Resolved %s -> %s.%s v%s (Repo: %s)\n", target, selected.Org, selected.Name, selected.Version, selected.Repo.Name)

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
		fmt.Printf("Installing %s.%s v%s...\n", pkg.Org, pkg.Name, pkg.Version)
		if err := downloadAndExtract(pkg); err != nil {
			fmt.Printf("Installation failed for %s: %v\n", pkg.Name, err)
			os.Exit(1)
		}
	}

	fmt.Println("All packages installed and mounted successfully!")
}
