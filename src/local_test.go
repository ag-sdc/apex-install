package main

import (
	"os"
	"path/filepath"
	"testing"
)

func createTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestResolveLocalTargetCapabilities(t *testing.T) {
	tmpRoot := t.TempDir()
	installRoot := filepath.Join(tmpRoot, "apex")
	cacheDir := filepath.Join(tmpRoot, "cache")
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		t.Fatal(err)
	}

	// Empty mountpoint directory must be ignored as unmounted/not installed.
	if err := os.MkdirAll(filepath.Join(installRoot, "org.blisslabs.empty"), 0755); err != nil {
		t.Fatal(err)
	}
	// Merged system dir without manifest must not be misidentified as a package.
	createTestFile(t, filepath.Join(installRoot, "lib", "pkgconfig", "merged_only.pc"), "Name: merged\n")

	// Package with pc:, cmake:, bin:, py3.11:, jar:, header:, and exact/partial .so.
	zlibPkg := filepath.Join(installRoot, "com.android.zlib.dev")
	createTestFile(t, filepath.Join(zlibPkg, "lib64", "pkgconfig", "zlib.pc"), "Name: zlib\n")
	createTestFile(t, filepath.Join(zlibPkg, "lib64", "libz.so"), "ELF")
	createTestFile(t, filepath.Join(zlibPkg, "include", "zlib.h"), "/* zlib */\n")

	toolPkg := filepath.Join(installRoot, "org.blisslabs.ag.devtools")
	createTestFile(t, filepath.Join(toolPkg, "bin", "mytool"), "#!/bin/sh\n")
	createTestFile(t, filepath.Join(toolPkg, "lib64", "cmake", "MyTool", "MyToolConfig.cmake"), "# cmake\n")
	createTestFile(t, filepath.Join(toolPkg, "lib", "python3.11", "site-packages", "yaml", "__init__.py"), "")
	createTestFile(t, filepath.Join(toolPkg, "share", "java", "core-all.jar"), "JAR")
	createTestFile(t, filepath.Join(toolPkg, "lib64", "libzstd.so"), "ELF")

	// Empty mountpoint in providers must NOT resolve, while populated package in providers MUST resolve.
	createTestFile(t, filepath.Join(cacheDir, "ci.providers"),
		"pc:virtual-zlib x86_64 v1 35 com.android.zlib.dev 1.0\n"+
			"pc:unmounted x86_64 v1 35 org.blisslabs.empty 1.0\n"+
			"alias:customtools x86_64 v1 35 org.blisslabs.ag.devtools 1.0\n")

	// Merged symlink pointing into installed package.
	mergedPc := filepath.Join(installRoot, "lib64", "pkgconfig", "zlib-symlink.pc")
	if err := os.MkdirAll(filepath.Dir(mergedPc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(zlibPkg, "lib64", "pkgconfig", "zlib.pc"), mergedPc); err != nil {
		t.Fatal(err)
	}

	opts := localQueryOptions{
		installPaths: []string{installRoot},
		mergePaths:   []string{installRoot},
		dbCacheDirs:  []string{cacheDir},
	}

	tests := []struct {
		query   string
		wantPkg string
		wantOK  bool
	}{
		{"pc:zlib", "com.android.zlib.dev", true},
		{"pc:zlib>=1.2", "com.android.zlib.dev", true},
		{"pc:zlib-symlink", "com.android.zlib.dev", true},
		{"pc:virtual-zlib", "com.android.zlib.dev", true},
		{"pc:unmounted", "", false},
		{"pc:merged_only", "", false},
		{"cmake:MyTool", "org.blisslabs.ag.devtools", true},
		{"bin:mytool", "org.blisslabs.ag.devtools", true},
		{"py3.11:yaml", "org.blisslabs.ag.devtools", true},
		{"jar:core-all", "org.blisslabs.ag.devtools", true},
		{"header:zlib", "com.android.zlib.dev", true},
		{"alias:devtools", "org.blisslabs.ag.devtools", true},
		{"alias:customtools", "org.blisslabs.ag.devtools", true},
		{"com.android.zlib.dev", "com.android.zlib.dev", true},
		{"org.blisslabs.empty", "", false},
		{"libz.so", "com.android.zlib.dev", true},
		{"libz", "com.android.zlib.dev", true},
		{"pc:nonexistent", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			gotPkg, _, gotOK := resolveLocalTarget(tc.query, opts)
			if gotOK != tc.wantOK || gotPkg != tc.wantPkg {
				t.Errorf("resolveLocalTarget(%q) = (%q, %v), want (%q, %v)",
					tc.query, gotPkg, gotOK, tc.wantPkg, tc.wantOK)
			}
		})
	}
}

func TestCollectLocalQueryOptionsRespectsApexSearchPathAndRootFallback(t *testing.T) {
	oldActive := ActiveConfig
	defer func() { ActiveConfig = oldActive }()

	ActiveConfig = ContextConfig{
		InstallPath: "/tmp/user-mnt",
		MergePath:   "/tmp/user-mnt",
		DBCacheDir:  "/tmp/user-cache",
	}
	cfg := &ApexConfig{
		Root: ContextConfig{
			InstallPath: "/apex",
			MergePath:   "/apex",
			DBCacheDir:  "/var/cache/apex/sync",
		},
	}

	t.Setenv("APEX_SEARCH_PATH", "/custom/a:/custom/b")

	opts := collectLocalQueryOptions(cfg, false, false, "")
	wantInstall := []string{"/custom/a", "/custom/b", "/tmp/user-mnt", "/apex"}
	if len(opts.installPaths) != len(wantInstall) {
		t.Fatalf("installPaths = %v, want %v", opts.installPaths, wantInstall)
	}
	for i := range wantInstall {
		if opts.installPaths[i] != wantInstall[i] {
			t.Errorf("installPaths[%d] = %q, want %q", i, opts.installPaths[i], wantInstall[i])
		}
	}

	isolatedOpts := collectLocalQueryOptions(cfg, false, true, "/chroot")
	wantIso := []string{"/custom/a", "/custom/b", "/chroot/apex", "/tmp/user-mnt"}
	if len(isolatedOpts.installPaths) != len(wantIso) {
		t.Fatalf("isolated installPaths = %v, want %v", isolatedOpts.installPaths, wantIso)
	}
	for i := range wantIso {
		if isolatedOpts.installPaths[i] != wantIso[i] {
			t.Errorf("isolated installPaths[%d] = %q, want %q", i, isolatedOpts.installPaths[i], wantIso[i])
		}
	}
}
