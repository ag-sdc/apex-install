package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseApiLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 29},
		{"0", 29},
		{"29", 29},
		{"30", 30},
		{"32", 32},
		{"35", 35},
		{"invalid", 29},
		{"  32  ", 32},
		{"API 32", 32},
		{"api 35", 35},
		{"v33", 33},
		{"api0", 29},
	}

	for _, tt := range tests {
		got := parseApiLevel(tt.input)
		if got != tt.expected {
			t.Errorf("parseApiLevel(%q) = %d; want %d", tt.input, got, tt.expected)
		}
	}
}

func TestFlagParsingAndAliases(t *testing.T) {
	tests := []struct {
		name              string
		args              []string
		wantApiLevel      int
		wantApiLevelExact int
	}{
		{
			name:              "api-level flag",
			args:              []string{"--api-level", "33"},
			wantApiLevel:      33,
			wantApiLevelExact: 0,
		},
		{
			name:              "api alias flag",
			args:              []string{"--api", "34"},
			wantApiLevel:      34,
			wantApiLevelExact: 0,
		},
		{
			name:              "api-level-exact flag",
			args:              []string{"--api-level-exact", "32"},
			wantApiLevel:      0,
			wantApiLevelExact: 32,
		},
		{
			name:              "api-exact alias flag",
			args:              []string{"--api-exact", "35"},
			wantApiLevel:      0,
			wantApiLevelExact: 35,
		},
		{
			name:              "both api and api-exact specified",
			args:              []string{"--api", "30", "--api-exact", "32"},
			wantApiLevel:      30,
			wantApiLevelExact: 32,
		},
		{
			name:              "both api-level and api-level-exact specified",
			args:              []string{"--api-level", "30", "--api-level-exact", "35"},
			wantApiLevel:      30,
			wantApiLevelExact: 35,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			apiLevel := fs.Int("api-level", 0, "")
			fs.IntVar(apiLevel, "api", 0, "")
			apiLevelExact := fs.Int("api-level-exact", 0, "")
			fs.IntVar(apiLevelExact, "api-exact", 0, "")

			err := fs.Parse(tt.args)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if *apiLevel != tt.wantApiLevel {
				t.Errorf("apiLevel = %d, want %d", *apiLevel, tt.wantApiLevel)
			}
			if *apiLevelExact != tt.wantApiLevelExact {
				t.Errorf("apiLevelExact = %d, want %d", *apiLevelExact, tt.wantApiLevelExact)
			}
		})
	}
}

func TestCandidateApiFilteringAndPrecedence(t *testing.T) {
	cands := []*PackageCandidate{
		{Name: "pkg1", ApiLevel: "30"},
		{Name: "pkg2", ApiLevel: "32"},
		{Name: "pkg3", ApiLevel: "35"},
	}

	filter := func(candidates []*PackageCandidate, apiLevelExact, apiLevel int) []*PackageCandidate {
		var res []*PackageCandidate
		for _, c := range candidates {
			if matchCandidateApi(c, apiLevelExact, apiLevel) {
				res = append(res, c)
			}
		}
		return res
	}

	// 1. Exact match 32
	f := filter(cands, 32, 0)
	if len(f) != 1 || f[0].Name != "pkg2" {
		t.Errorf("exact 32: got %d candidates, want 1 (pkg2)", len(f))
	}

	// 2. Exact match 35
	f = filter(cands, 35, 0)
	if len(f) != 1 || f[0].Name != "pkg3" {
		t.Errorf("exact 35: got %d candidates, want 1 (pkg3)", len(f))
	}

	// 3. Max API level 32
	f = filter(cands, 0, 32)
	if len(f) != 2 || f[0].Name != "pkg1" || f[1].Name != "pkg2" {
		t.Errorf("max 32: got %d candidates, want 2 (pkg1, pkg2)", len(f))
	}

	// 4. Precedence: Exact 35 takes precedence over Max 30
	f = filter(cands, 35, 30)
	if len(f) != 1 || f[0].Name != "pkg3" {
		t.Errorf("precedence exact 35 over max 30: got %d candidates, want 1 (pkg3)", len(f))
	}

	// 5. Precedence: Exact 32 takes precedence over Max 35
	f = filter(cands, 32, 35)
	if len(f) != 1 || f[0].Name != "pkg2" {
		t.Errorf("precedence exact 32 over max 35: got %d candidates, want 1 (pkg2)", len(f))
	}
}

func TestLocalSearchApiFilteringAndPrecedence(t *testing.T) {
	pkgs := []*installedPkgInfo{
		{name: "pkg-no-api", apiLevel: ""},
		{name: "pkg-api-30", apiLevel: "30"},
		{name: "pkg-api-32", apiLevel: "32"},
		{name: "pkg-api-35", apiLevel: "35"},
	}

	filter := func(installed []*installedPkgInfo, apiLevelExact, apiLevel int) []*installedPkgInfo {
		var res []*installedPkgInfo
		for _, p := range installed {
			if matchInstalledPkgApi(p, apiLevelExact, apiLevel) {
				res = append(res, p)
			}
		}
		return res
	}

	// 1. No flags: all kept
	f := filter(pkgs, 0, 0)
	if len(f) != 4 {
		t.Errorf("no flags: got %d, want 4", len(f))
	}

	// 2. Exact 35: only pkg-api-35
	f = filter(pkgs, 35, 0)
	if len(f) != 1 || f[0].name != "pkg-api-35" {
		t.Errorf("exact 35: got %d, want 1 (pkg-api-35)", len(f))
	}

	// 3. Exact 32: only pkg-api-32
	f = filter(pkgs, 32, 0)
	if len(f) != 1 || f[0].name != "pkg-api-32" {
		t.Errorf("exact 32: got %d, want 1 (pkg-api-32)", len(f))
	}

	// 4. Exact 29 on pkg with empty apiLevel: empty apiLevel must not match
	f = filter(pkgs, 29, 0)
	if len(f) != 0 {
		t.Errorf("exact 29 with no 29 pkgs: got %d, want 0", len(f))
	}

	// 5. Max 32: excludes pkg-no-api and pkg-api-35
	f = filter(pkgs, 0, 32)
	if len(f) != 2 || f[0].name != "pkg-api-30" || f[1].name != "pkg-api-32" {
		t.Errorf("max 32: got %d, want 2", len(f))
	}

	// 6. Precedence: Exact 35 over Max 30
	f = filter(pkgs, 35, 30)
	if len(f) != 1 || f[0].name != "pkg-api-35" {
		t.Errorf("precedence exact 35 over max 30: got %d, want 1 (pkg-api-35)", len(f))
	}
}

func TestSortCandidatesVersion(t *testing.T) {
	cands := []*PackageCandidate{
		{Name: "pkg", Version: "1000", ApiLevel: "32"},
		{Name: "pkg", Version: "2000", ApiLevel: "32"},
		{Name: "pkg", Version: "1500", ApiLevel: "32"},
	}
	sortCandidates(cands, "")
	if cands[0].Version != "2000" || cands[1].Version != "1500" || cands[2].Version != "1000" {
		t.Errorf("sortCandidates did not sort integer versionCodes correctly: got %v, %v, %v",
			cands[0].Version, cands[1].Version, cands[2].Version)
	}

	semverCands := []*PackageCandidate{
		{Name: "pkg", Version: "1.0.0", ApiLevel: "32"},
		{Name: "pkg", Version: "1.2.0", ApiLevel: "32"},
		{Name: "pkg", Version: "1.1.0", ApiLevel: "32"},
	}
	sortCandidates(semverCands, "")
	if semverCands[0].Version != "1.2.0" || semverCands[1].Version != "1.1.0" || semverCands[2].Version != "1.0.0" {
		t.Errorf("sortCandidates did not sort semver correctly: got %v, %v, %v",
			semverCands[0].Version, semverCands[1].Version, semverCands[2].Version)
	}
}

func TestPreProcessArgsFlagsBeforeSubcommands(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "flags before search",
			input:    []string{"apexm", "--api-exact", "32", "search", "bzip2"},
			expected: []string{"apexm", "--api-exact", "32", "-S", "-s", "bzip2"},
		},
		{
			name:     "search before flags",
			input:    []string{"apexm", "search", "--api-exact", "32", "bzip2"},
			expected: []string{"apexm", "-S", "-s", "--api-exact", "32", "bzip2"},
		},
		{
			name:     "flags before install",
			input:    []string{"apexm", "--api", "30", "install", "foo"},
			expected: []string{"apexm", "--api", "30", "-S", "foo"},
		},
		{
			name:     "flags before download",
			input:    []string{"apexm", "--api-level-exact", "32", "download", "foo"},
			expected: []string{"apexm", "--api-level-exact", "32", "-S", "-d", "foo"},
		},
		{
			name:     "flags before local search",
			input:    []string{"apexm", "--api-exact", "35", "local", "-s"},
			expected: []string{"apexm", "--api-exact", "35", "-Q", "-s"},
		},
		{
			name:     "existing -Ss flag unchanged",
			input:    []string{"apexm", "-Ss", "--api-exact", "32", "bzip2"},
			expected: []string{"apexm", "-Ss", "--api-exact", "32", "bzip2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()

			os.Args = append([]string{}, tt.input...)
			preProcessArgs()

			if !reflect.DeepEqual(os.Args, tt.expected) {
				t.Errorf("preProcessArgs() = %v, want %v", os.Args, tt.expected)
			}
		})
	}
}
