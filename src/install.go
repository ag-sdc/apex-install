package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

	ext := resolveExtension(cand)
	orgPath := strings.ReplaceAll(cand.Name, ".", "/")
	pkgFilename := fmt.Sprintf("%s.%s", cand.Version, ext)
	urlStr := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)

	fmt.Printf("Downloading %s...\n", urlStr)
	resp, err := doReq(cand.Repo, urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Just in case it was a fluke, fallback to the other extension
		fallbackExt := "apex"
		if ext == "apex" {
			fallbackExt = "capex"
		}
		pkgFilename = fmt.Sprintf("%s.%s", cand.Version, fallbackExt)
		urlStr = fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)
		fmt.Printf("Fallback downloading %s...\n", urlStr)
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

	targetDir := fmt.Sprintf("/opt/apex/%s.apex", cand.Name)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v (are you running as root?)", targetDir, err)
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

	mountPoint := fmt.Sprintf("/apex/%s", cand.Name)
	os.MkdirAll(mountPoint, 0755)

	fmt.Printf("Mounting %s to %s...\n", loopDev, mountPoint)
	cmd = exec.Command("mount", "-t", "auto", "-o", "ro", loopDev, mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to mount %s to %s: %v\nOutput: %s", loopDev, mountPoint, err, string(output))
	}

	createSymlinks(cand.Name)

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

func isWritable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return false
	}
	if info.Mode().Perm()&(1<<(uint(7))) == 0 {
		return false
	}
	testFile := filepath.Join(path, ".test")
	f, err := os.Create(testFile)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(testFile)
	return true
}

func doUpdate(repos []*RepoConfig) {
	syncDir := "/var/cache/apex/sync"
	// Check if we can write to /var/cache/apex/sync
	err := os.MkdirAll(syncDir, 0755)
	if err != nil || !isWritable(syncDir) {
		os.MkdirAll(syncDir, 0755)
	}
	for _, repo := range repos {
		fmt.Printf("Updating repo %s...\n", repo.Name)
		errProv := tryDownload(repo, syncDir, ".providers", []string{".providers", ".providers.tar.gz"})
		if errProv != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", errProv)
		}
		errDb := tryDownload(repo, syncDir, ".db", []string{".db", ".db.tar.gz"})
		if errDb != nil {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", errDb)
		}
	}
}
