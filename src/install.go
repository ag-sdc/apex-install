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

func detectFSType(payloadImg string) string {
	f, err := os.Open(payloadImg)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Check EROFS (magic: E2 E1 F5 E0 at offset 1024)
	f.Seek(1024, 0)
	magic := make([]byte, 4)
	f.Read(magic)
	if magic[0] == 0xE2 && magic[1] == 0xE1 && magic[2] == 0xF5 && magic[3] == 0xE0 {
		return "erofs"
	}

	// Check EXT4 (magic: 53 EF at offset 1080)
	f.Seek(1080, 0)
	magic = make([]byte, 2)
	f.Read(magic)
	if magic[0] == 0x53 && magic[1] == 0xEF {
		return "ext4"
	}

	return ""
}

type extractCmd struct {
	Name string
	Args []string
}

func extractApex(payloadImg, mountPoint string) error {
	fsType := detectFSType(payloadImg)
	LogV("Detected filesystem type: %s", fsType)

	var cmds []extractCmd

	switch fsType {
	case "erofs":
		cmds = append(cmds, extractCmd{"fsck.erofs", []string{fmt.Sprintf("--extract=%s", mountPoint), payloadImg}})
	case "ext4":
		cmds = append(cmds, extractCmd{"debugfs", []string{"-R", fmt.Sprintf("rdump / %s", mountPoint), payloadImg}})
	}

	cmds = append(cmds, extractCmd{"7z", []string{"x", payloadImg, fmt.Sprintf("-o%s", mountPoint)}})

	var errs []string
	for _, cmdDef := range cmds {
		LogV("Extracting via %s...", cmdDef.Name)
		cmd := exec.Command(cmdDef.Name, cmdDef.Args...)
		output, err := cmd.CombinedOutput()

		if len(output) > 0 {
			LogV("Output from %s:\n%s", cmdDef.Name, string(output))
		}

		if err == nil {
			return nil
		}
		LogV("%s failed: %v", cmdDef.Name, err)
		errs = append(errs, fmt.Sprintf("%s failed: %v", cmdDef.Name, err))
	}

	return fmt.Errorf("all extraction methods failed for %s:\n%s", fsType, strings.Join(errs, "\n"))
}

func mountApex(payloadImg, mountPoint string) error {
	cmd := exec.Command("losetup", "-f", "--show", payloadImg)
	cmdOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("losetup failed: %v", err)
	}
	loopDev := strings.TrimSpace(string(cmdOut))

	LogV("Mounting %s to %s...", loopDev, mountPoint)
	cmd = exec.Command("mount", "-t", "auto", "-o", "ro", loopDev, mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to mount %s to %s: %v\nOutput: %s", loopDev, mountPoint, err, string(output))
	}
	return nil
}

func fuseApex(payloadImg, mountPoint string) error {
	LogV("Mounting %s to %s via fuse2fs...", payloadImg, mountPoint)
	cmd := exec.Command("fuse2fs", "-o", "ro", payloadImg, mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fuse2fs failed: %v\nOutput: %s", err, string(output))
	}
	return nil
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

	ext := resolveExtension(cand)
	orgPath := strings.ReplaceAll(cand.Name, ".", "/")
	pkgFilename := fmt.Sprintf("%s.%s", cand.Version, ext)
	urlStr := fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)

	LogV("Downloading %s...", urlStr)
	resp, err := doReq(cand.Repo, urlStr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fallbackExt := "apex"
		if ext == "apex" {
			fallbackExt = "capex"
		}
		pkgFilename = fmt.Sprintf("%s.%s", cand.Version, fallbackExt)
		urlStr = fmt.Sprintf("%s/%s/%s/%s/%s", strings.TrimRight(cand.Repo.URL, "/"), archSegment, apiLevelSegment, orgPath, pkgFilename)
		LogV("Fallback downloading %s...", urlStr)
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

	targetDir := filepath.Join(ActiveConfig.DownloadPath, fmt.Sprintf("%s.apex", cand.Name))
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", targetDir, err)
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

	mountPoint := filepath.Join(ActiveConfig.InstallPath, cand.Name)
	os.MkdirAll(mountPoint, 0755)

	mountMode := ActiveConfig.MountMode
	switch mountMode {
	case "auto":
		for {
			if os.Geteuid() == 0 {
				err = mountApex(payloadImg, mountPoint)
				if err == nil {
					break
				}
				LogV("Mount failed: %v. Falling back to fuse...", err)
			}

			err = fuseApex(payloadImg, mountPoint)
			if err == nil {
				break
			}
			LogV("Fuse failed: %v. Falling back to extract...", err)

			err = extractApex(payloadImg, mountPoint)
			if err == nil {
				break
			}

			return fmt.Errorf("all mount/fuse/extract methods failed for %s", cand.Name)
		}
	case "mount":
		err = mountApex(payloadImg, mountPoint)
		if err != nil {
			return err
		}
	case "fuse":
		err = fuseApex(payloadImg, mountPoint)
		if err != nil {
			return err
		}
	case "extract":
		err = extractApex(payloadImg, mountPoint)
		if err != nil {
			return err
		}
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
	mountPoint := filepath.Join(ActiveConfig.InstallPath, pkgName)
	mergePoint := ActiveConfig.MergePath

	includesSrc := filepath.Join(mountPoint, "includes")
	if _, err := os.Stat(includesSrc); os.IsNotExist(err) {
		includesSrc = filepath.Join(mountPoint, "include") // Fallback
	}
	linkContents(includesSrc, filepath.Join(mergePoint, "include"))

	linkContents(filepath.Join(mountPoint, "lib"), filepath.Join(mergePoint, "lib"))

	pkgConfigDash := filepath.Join(mountPoint, "lib", "pkg-config")
	if stat, err := os.Stat(pkgConfigDash); err == nil && stat.IsDir() {
		linkContents(pkgConfigDash, filepath.Join(mergePoint, "lib", "pkgconfig"))
	}

	linkContents(filepath.Join(mountPoint, "bin"), filepath.Join(mergePoint, "bin"))
	linkContents(filepath.Join(mountPoint, "share"), filepath.Join(mergePoint, "share"))
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
	syncDir := ActiveConfig.DBCacheDir
	err := os.MkdirAll(syncDir, 0755)
	if err != nil || !isWritable(syncDir) {
		os.MkdirAll(syncDir, 0755)
	}
	for _, repo := range repos {
		LogV("Updating repo %s...", repo.Name)
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
