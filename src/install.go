package main

import (
	"archive/zip"
	"bufio"
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

func extractZip(zipFile, targetDir string) error {
	zr, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer zr.Close()

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

	mountPoint := filepath.Join(ActiveConfig.InstallPath, cand.Name)
	if _, err := os.Stat(mountPoint); err == nil {
		uninstallApex(cand.Name)
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

	targetDir := filepath.Join(ActiveConfig.DownloadPath, fmt.Sprintf("%s@%s.apex", cand.Name, cand.Version))
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

	if DownloadOnlyMode {
		LogV("Download-only mode active. Skipping extraction for %s", cand.Name)
		return nil
	}

	err = extractZip(tmpFile, targetDir)
	os.Remove(tmpFile)
	if err != nil {
		return err
	}

	originalApex := filepath.Join(targetDir, "original_apex")
	if _, err := os.Stat(originalApex); err == nil {
		LogV("Detected CAPEX package, extracting original_apex...")
		err = extractZip(originalApex, targetDir)
		os.Remove(originalApex)
		if err != nil {
			return err
		}
	}

	return installLocalApex(cand.Name, targetDir)
}

func installLocalApex(pkgName, targetDir string) error {
	payloadImg := filepath.Join(targetDir, "apex_payload.img")
	if _, err := os.Stat(payloadImg); err != nil {
		return fmt.Errorf("apex_payload.img not found in %s", targetDir)
	}

	mountPoint := filepath.Join(ActiveConfig.InstallPath, pkgName)
	os.MkdirAll(mountPoint, 0755)

	var err error
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

			return fmt.Errorf("all mount/fuse/extract methods failed for %s", pkgName)
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

	createSymlinks(pkgName)
	return nil
}

func linkContentsTracked(srcDir, destDir string, manifest *[]string) {
	if stat, err := os.Stat(srcDir); err != nil || !stat.IsDir() {
		return
	}
	os.MkdirAll(destDir, 0755)
	entries, _ := os.ReadDir(srcDir)
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		destPath := filepath.Join(destDir, entry.Name())

		if entry.IsDir() {
			linkContentsTracked(srcPath, destPath, manifest)
		} else {
			os.Remove(destPath)
			if err := os.Symlink(srcPath, destPath); err == nil && manifest != nil {
				*manifest = append(*manifest, destPath)
			}
		}
	}
}

func linkContents(srcDir, destDir string) {
	linkContentsTracked(srcDir, destDir, nil)
}

func unlinkContents(srcDir, destDir string) {
	if stat, err := os.Stat(srcDir); err != nil || !stat.IsDir() {
		return
	}
	entries, _ := os.ReadDir(srcDir)
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		destPath := filepath.Join(destDir, entry.Name())

		if entry.IsDir() {
			unlinkContents(srcPath, destPath)
		} else {
			if linkTarget, err := os.Readlink(destPath); err == nil {
				if linkTarget == srcPath {
					os.Remove(destPath)
				}
			}
		}
	}
}

func uninstallApex(pkgName string) error {
	LogV("Uninstalling previous version of %s...", pkgName)
	mountPoint := filepath.Join(ActiveConfig.InstallPath, pkgName)
	mergePoint := ActiveConfig.MergePath

	// 1. Surgical unlinking using manifest if present
	manifestPath := filepath.Join(ActiveConfig.InstallPath, ".installed", pkgName+".manifest")
	if f, err := os.Open(manifestPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			destPath := strings.TrimSpace(scanner.Text())
			if destPath == "" {
				continue
			}
			if target, err := os.Readlink(destPath); err == nil && strings.HasPrefix(target, mountPoint) {
				os.Remove(destPath)
			}
		}
		f.Close()
		os.Remove(manifestPath)
	}

	// 2. Fallback standard unlinks
	includesSrc := filepath.Join(mountPoint, "includes")
	if _, err := os.Stat(includesSrc); os.IsNotExist(err) {
		includesSrc = filepath.Join(mountPoint, "include") // Fallback
	}
	unlinkContents(includesSrc, filepath.Join(mergePoint, "include"))
	unlinkContents(filepath.Join(mountPoint, "lib"), filepath.Join(mergePoint, "lib"))
	unlinkContents(filepath.Join(mountPoint, "lib64"), filepath.Join(mergePoint, "lib64"))
	unlinkContents(filepath.Join(mountPoint, "lib64"), filepath.Join(mergePoint, "lib"))

	pkgConfigDash := filepath.Join(mountPoint, "lib", "pkg-config")
	if stat, err := os.Stat(pkgConfigDash); err == nil && stat.IsDir() {
		unlinkContents(pkgConfigDash, filepath.Join(mergePoint, "lib", "pkgconfig"))
	}
	unlinkContents(filepath.Join(mountPoint, "lib", "pkgconfig"), filepath.Join(mergePoint, "lib", "pkgconfig"))
	unlinkContents(filepath.Join(mountPoint, "lib64", "pkgconfig"), filepath.Join(mergePoint, "lib", "pkgconfig"))
	unlinkContents(filepath.Join(mountPoint, "share", "pkgconfig"), filepath.Join(mergePoint, "share", "pkgconfig"))
	unlinkContents(filepath.Join(mountPoint, "bin"), filepath.Join(mergePoint, "bin"))
	unlinkContents(filepath.Join(mountPoint, "share"), filepath.Join(mergePoint, "share"))

	exec.Command("umount", mountPoint).Run()
	exec.Command("fusermount", "-u", mountPoint).Run()
	exec.Command("fusermount3", "-u", mountPoint).Run()

	os.RemoveAll(mountPoint)
	os.RemoveAll(filepath.Join(ActiveConfig.DownloadPath, pkgName+".apex"))
	os.RemoveAll(filepath.Join(ActiveConfig.DownloadPath, pkgName+".capex"))
	if matches, err := filepath.Glob(filepath.Join(ActiveConfig.DownloadPath, pkgName+"@*.apex")); err == nil {
		for _, m := range matches {
			os.RemoveAll(m)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(ActiveConfig.DownloadPath, pkgName+"@*.capex")); err == nil {
		for _, m := range matches {
			os.RemoveAll(m)
		}
	}

	return nil
}

func createSymlinks(pkgName string) {
	mountPoint := filepath.Join(ActiveConfig.InstallPath, pkgName)
	mergePoint := ActiveConfig.MergePath
	var manifest []string

	// 1. Headers
	includesSrc := filepath.Join(mountPoint, "includes")
	if _, err := os.Stat(includesSrc); os.IsNotExist(err) {
		includesSrc = filepath.Join(mountPoint, "include") // Fallback
	}
	linkContentsTracked(includesSrc, filepath.Join(mergePoint, "include"), &manifest)

	// 2. Lib & Lib64
	linkContentsTracked(filepath.Join(mountPoint, "lib"), filepath.Join(mergePoint, "lib"), &manifest)
	if _, err := os.Stat(filepath.Join(mountPoint, "lib64")); err == nil {
		linkContentsTracked(filepath.Join(mountPoint, "lib64"), filepath.Join(mergePoint, "lib64"), &manifest)
		linkContentsTracked(filepath.Join(mountPoint, "lib64"), filepath.Join(mergePoint, "lib"), &manifest)
	}

	// 3. Centralized Pkg-Config Pool
	pkgConfigDirs := []struct {
		src  string
		dest string
	}{
		{filepath.Join(mountPoint, "lib", "pkgconfig"), filepath.Join(mergePoint, "lib", "pkgconfig")},
		{filepath.Join(mountPoint, "lib64", "pkgconfig"), filepath.Join(mergePoint, "lib", "pkgconfig")},
		{filepath.Join(mountPoint, "lib", "pkg-config"), filepath.Join(mergePoint, "lib", "pkgconfig")},
		{filepath.Join(mountPoint, "share", "pkgconfig"), filepath.Join(mergePoint, "share", "pkgconfig")},
	}
	for _, pc := range pkgConfigDirs {
		if stat, err := os.Stat(pc.src); err == nil && stat.IsDir() {
			linkContentsTracked(pc.src, pc.dest, &manifest)
		}
	}

	// 4. Bin & Share
	linkContentsTracked(filepath.Join(mountPoint, "bin"), filepath.Join(mergePoint, "bin"), &manifest)
	linkContentsTracked(filepath.Join(mountPoint, "share"), filepath.Join(mergePoint, "share"), &manifest)

	// 5. Dynamic directory walking for other top-level directories
	if entries, err := os.ReadDir(mountPoint); err == nil {
		skipDirs := map[string]bool{
			"bin": true, "lib": true, "lib64": true, "include": true, "includes": true, "share": true,
			"lost+found": true, "apex_manifest.json": true, "apex_manifest.pb": true,
			"apex_pubkey": true, "apex_build_info.pb": true, "AndroidManifest.xml": true,
		}
		for _, entry := range entries {
			if skipDirs[entry.Name()] {
				continue
			}
			srcEntry := filepath.Join(mountPoint, entry.Name())
			destEntry := filepath.Join(mergePoint, entry.Name())

			if entry.IsDir() {
				if entry.Name() == "opt" {
					// Atomic directory link for opt subtrees
					if optEntries, err := os.ReadDir(srcEntry); err == nil {
						os.MkdirAll(destEntry, 0755)
						for _, optTool := range optEntries {
							toolSrc := filepath.Join(srcEntry, optTool.Name())
							toolDest := filepath.Join(destEntry, optTool.Name())
							os.Remove(toolDest)
							if err := os.Symlink(toolSrc, toolDest); err == nil {
								manifest = append(manifest, toolDest)
							}
						}
					}
				} else {
					linkContentsTracked(srcEntry, destEntry, &manifest)
				}
			}
		}
	}

	// Save install manifest
	metaDir := filepath.Join(ActiveConfig.InstallPath, ".installed")
	os.MkdirAll(metaDir, 0755)
	manifestPath := filepath.Join(metaDir, pkgName+".manifest")
	if f, err := os.Create(manifestPath); err == nil {
		for _, link := range manifest {
			f.WriteString(link + "\n")
		}
		f.Close()
	}
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
