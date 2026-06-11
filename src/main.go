package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

type Config struct {
	RepoURL   string
	RepoName  string
	AuthToken string
	AuthBasic string
	Arch      string
}

func readConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	cfg := &Config{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "REPO_URL":
			cfg.RepoURL = v
		case "REPO_NAME":
			cfg.RepoName = v
		case "AUTH_TOKEN":
			cfg.AuthToken = v
		case "AUTH_BASIC":
			cfg.AuthBasic = v
		case "ARCH":
			cfg.Arch = v
		}
	}
	return cfg, scanner.Err()
}

func doReq(cfg *Config, urlStr string) (*http.Response, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	} else if cfg.AuthBasic != "" {
		req.Header.Set("Authorization", "Basic "+cfg.AuthBasic)
	}

	client := &http.Client{}
	return client.Do(req)
}

func getProviders(cfg *Config, libName string) ([]string, error) {
	urlStr := fmt.Sprintf("%s/%s.providers.tar.gz", strings.TrimRight(cfg.RepoURL, "/"), cfg.RepoName)
	resp, err := doReq(cfg, urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download providers: %s", resp.Status)
	}

	gzr, err := gzip.NewReader(resp.Body)
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
		if header.Name == "providers" || header.Name == cfg.RepoName+".providers" {
			scanner := bufio.NewScanner(tr)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, libName+":") {
					pkgs := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, libName+":")))
					return pkgs, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("library %s not found in providers", libName)
}

type PackageCandidate struct {
	Name      string
	Version   string
	Org       string
	Arch      string
	MicroArch string
	Priority  int // lower is better
}

func getCandidates(cfg *Config, pkgNames []string) ([]PackageCandidate, error) {
	urlStr := fmt.Sprintf("%s/%s.db.tar.gz", strings.TrimRight(cfg.RepoURL, "/"), cfg.RepoName)
	resp, err := doReq(cfg, urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download db: %s", resp.Status)
	}

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	pkgOrder := make(map[string]int)
	for i, p := range pkgNames {
		pkgOrder[p] = i
	}

	var candidates []PackageCandidate
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
			// Read desc
			content, err := io.ReadAll(tr)
			if err != nil {
				continue
			}
			lines := strings.Split(string(content), "\n")
			var name, version, arch, microarch, org string
			for i := 0; i < len(lines); i++ {
				line := strings.TrimSpace(lines[i])
				if line == "%NAME%" && i+1 < len(lines) {
					name = strings.TrimSpace(lines[i+1])
				} else if line == "%VERSION%" && i+1 < len(lines) {
					version = strings.TrimSpace(lines[i+1])
				} else if line == "%ARCH%" && i+1 < len(lines) {
					arch = strings.TrimSpace(lines[i+1])
				} else if line == "%MICROARCH%" && i+1 < len(lines) {
					microarch = strings.TrimSpace(lines[i+1])
				} else if line == "%ORG%" && i+1 < len(lines) {
					org = strings.TrimSpace(lines[i+1])
				}
			}

			if prio, ok := pkgOrder[name]; ok {
				if cfg.Arch == "" || cfg.Arch == arch || arch == "any" {
					candidates = append(candidates, PackageCandidate{
						Name:      name,
						Version:   version,
						Org:       org,
						Arch:      arch,
						MicroArch: microarch,
						Priority:  prio,
					})
				}
			}
		}
	}
	return candidates, nil
}

func sortCandidates(candidates []PackageCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		if candidates[i].MicroArch != candidates[j].MicroArch {
			return candidates[i].MicroArch < candidates[j].MicroArch
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

func downloadAndExtract(cfg *Config, cand PackageCandidate) error {
	// e.g. <arch>-<microarch>/reverse/domain/org/name/version.apex
	// microarch formatting: replace _ with . (e.g. v8_0 -> v8.0)
	dirMicroArch := strings.ReplaceAll(cand.MicroArch, "_", ".")
	
	archSegment := cand.Arch
	if dirMicroArch != "" {
		archSegment = fmt.Sprintf("%s-%s", cand.Arch, dirMicroArch)
	}

	orgPath := strings.ReplaceAll(cand.Org, ".", "/")
	pkgFilename := fmt.Sprintf("%s-%s.capex", cand.Name, cand.Version)
	urlStr := fmt.Sprintf("%s/%s/%s/%s", strings.TrimRight(cfg.RepoURL, "/"), archSegment, orgPath, pkgFilename)

	fmt.Printf("Downloading %s...\n", urlStr)
	resp, err := doReq(cfg, urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// fallback to .apex
		pkgFilename = fmt.Sprintf("%s-%s.apex", cand.Name, cand.Version)
		urlStr = fmt.Sprintf("%s/%s/%s/%s", strings.TrimRight(cfg.RepoURL, "/"), archSegment, orgPath, pkgFilename)
		resp2, err := doReq(cfg, urlStr)
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
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

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

	fmt.Printf("Extracting to %s...\n", targetDir)
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
		if err != nil {
			outFile.Close()
			continue
		}
		io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
	}
	zr.Close()
	os.Remove(tmpFile)

	payloadImg := filepath.Join(targetDir, "apex_payload.img")
	if _, err := os.Stat(payloadImg); err != nil {
		return fmt.Errorf("apex_payload.img not found in package")
	}

	fmt.Println("Setting up loop device...")
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mount failed: %v", err)
	}

	fmt.Println("Successfully installed and mounted!")
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: apex-install <libname.so>")
		os.Exit(1)
	}
	libName := os.Args[1]

	cfg, err := readConfig("/etc/apex/repo.conf")
	if err != nil {
		// Fallback to local test config
		cfg, err = readConfig("repo.conf")
		if err != nil {
			fmt.Printf("Failed to read config: %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.RepoURL == "" || cfg.RepoName == "" {
		fmt.Println("REPO_URL and REPO_NAME must be set in config")
		os.Exit(1)
	}

	fmt.Printf("Searching for %s in %s...\n", libName, cfg.RepoName)
	pkgs, err := getProviders(cfg, libName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Found providers: %v\n", pkgs)

	candidates, err := getCandidates(cfg, pkgs)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if len(candidates) == 0 {
		fmt.Println("No matching packages found for your architecture")
		os.Exit(1)
	}

	sortCandidates(candidates)

	fmt.Println("Candidate options:")
	for i, c := range candidates {
		fmt.Printf(" [%d] %s.%s v%s (Arch: %s, Micro: %s) Priority: %d\n", i+1, c.Org, c.Name, c.Version, c.Arch, c.MicroArch, c.Priority)
	}

	top := candidates[0]
	fmt.Printf("Auto-selecting best match: %s.%s v%s\n", top.Org, top.Name, top.Version)

	if err := downloadAndExtract(cfg, top); err != nil {
		fmt.Printf("Installation failed: %v\n", err)
		os.Exit(1)
	}
}
