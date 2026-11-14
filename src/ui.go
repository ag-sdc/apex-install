package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var NoProgressBar bool

type winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func getTerminalWidth() int {
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 20 {
			return n
		}
	}
	var ws winsize
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if err == 0 && ws.Col > 20 {
		return int(ws.Col)
	}
	return 80
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	const unit = 1024.0
	div, exp := int64(unit), 0
	for n := b / 1024; n >= 1024; n /= 1024 {
		div *= 1024
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}

type PacmanProgressReader struct {
	reader      io.Reader
	label       string
	total       int64
	downloaded  int64
	startTime   time.Time
	lastUpdate  time.Time
	isTerm      bool
	suppressBar bool
	mu          sync.Mutex
	finished    bool
}

func NewPacmanProgressReader(r io.Reader, label string, total int64) *PacmanProgressReader {
	return &PacmanProgressReader{
		reader:      r,
		label:       label,
		total:       total,
		startTime:   time.Now(),
		isTerm:      isTerminal(os.Stdout),
		suppressBar: NoProgressBar,
	}
}

func (pr *PacmanProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.mu.Lock()
		pr.downloaded += int64(n)
		now := time.Now()
		if !pr.suppressBar && pr.isTerm && (now.Sub(pr.lastUpdate) >= 80*time.Millisecond || err != nil || (pr.total > 0 && pr.downloaded >= pr.total)) {
			pr.render(false)
			pr.lastUpdate = now
		}
		pr.mu.Unlock()
	}
	if err != nil {
		pr.Finish()
	}
	return n, err
}

func (pr *PacmanProgressReader) render(final bool) {
	if pr.suppressBar {
		return
	}

	W := getTerminalWidth()
	if W < 40 {
		W = 80
	}

	label := pr.label
	maxLabel := 26
	if len(label) > maxLabel {
		label = label[:maxLabel-3] + "..."
	}
	prefixLabel := fmt.Sprintf(" %-26s", label)

	sizeStr := formatBytes(pr.downloaded)
	colSize := fmt.Sprintf("%10s", sizeStr)

	elapsed := time.Since(pr.startTime).Seconds()
	var speedStr string
	if elapsed > 0.05 {
		speedStr = formatBytes(int64(float64(pr.downloaded)/elapsed)) + "/s"
	} else {
		speedStr = "0 B/s"
	}
	colSpeed := fmt.Sprintf("%11s", speedStr)

	var colEta string
	if pr.total > 0 && pr.downloaded < pr.total && elapsed > 0.1 {
		rate := float64(pr.downloaded) / elapsed
		remSec := int(float64(pr.total-pr.downloaded) / rate)
		if remSec < 0 {
			remSec = 0
		}
		colEta = fmt.Sprintf("%02d:%02d", remSec/60, remSec%60)
	} else if final {
		colEta = fmt.Sprintf("%02d:%02d", int(elapsed)/60, int(elapsed)%60)
	} else {
		colEta = "--:--"
	}
	colEta = fmt.Sprintf("%6s", colEta)

	var colPct string
	if pr.total > 0 {
		pct := int((pr.downloaded * 100) / pr.total)
		if final || pct > 100 {
			pct = 100
		}
		colPct = fmt.Sprintf("%4d%%", pct)
	} else {
		if final {
			colPct = " 100%"
		} else {
			colPct = "  --%"
		}
	}

	left := prefixLabel + colSize + "  " + colSpeed + " " + colEta + " "
	right := " " + colPct

	barWidth := W - len(left) - len(right) - 2
	if barWidth < 5 {
		barWidth = 5
	}

	var bar string
	if pr.total > 0 {
		filled := int((float64(pr.downloaded) / float64(pr.total)) * float64(barWidth))
		if final || filled > barWidth {
			filled = barWidth
		}
		unfilled := barWidth - filled
		if unfilled < 0 {
			unfilled = 0
		}
		bar = "[" + strings.Repeat("#", filled) + strings.Repeat("-", unfilled) + "]"
	} else {
		if final {
			bar = "[" + strings.Repeat("#", barWidth) + "]"
		} else {
			bar = "[" + strings.Repeat("-", barWidth) + "]"
		}
	}

	line := left + bar + right
	if pr.isTerm {
		fmt.Print("\r" + line)
	} else if final {
		fmt.Println(line)
	}
}

func (pr *PacmanProgressReader) Finish() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.finished {
		return
	}
	pr.finished = true
	if pr.suppressBar {
		return
	}
	if pr.isTerm {
		pr.render(true)
		fmt.Println()
	} else {
		pr.render(true)
	}
}

func printHelp(op string) {
	switch op {
	case "sync":
		fmt.Print(`usage:  apexm {-S --sync} [options] [package(s)]
options:
  -c, --config <path>      set an alternate configuration file
  -d, --download           download packages but do not install/mount anything
  -i, --isolated           isolated mode: ignore system states (user) or operate on rootdir (system)
  -l, --library            treat targets as library names instead of package names
  -n, --name               show only package name or restrict search strictly to package name
  -r, --rootdir <path>     root directory for isolated system operations
  -s, --search             search remote repositories for matching strings
  -u, --update             download fresh package databases from the server
  -v, --verbose            be verbose
  -y, --noconfirm          do not ask for any confirmation
      --noprogressbar      do not show a progress bar when downloading files
      --arch <arch>        target architecture (required if --max-microarch is set)
      --max-microarch <v>  highest microarchitecture level to download
      --api-level <level>  highest API level to download (prioritizes higher api-level, min 29)
      --api <level>        alias for --api-level
      --api-level-exact <level>
                           filter packages strictly to an exact Android API level
      --api-exact <level>  alias for --api-level-exact
      --skip-abi-level <level>
                           skip ABI level (0: none, 1: bionic libc, 2: all NDK [default], 3: NDK + VNDK)
      --disable-skip-abis  alias for --skip-abi-level 0 (do not skip any ABIs)
      --skip-abi-custom <libs>
                           custom libraries to skip (comma-separated or repeated)
`)
	case "query":
		fmt.Print(`usage:  apexm {-Q --query} [options] [package(s)]
options:
  -c, --config <path>      set an alternate configuration file
  -i, --isolated           isolated mode: ignore system states (user) or operate on rootdir (system)
  -l, --library            query installed packages for a library name
  -n, --name               show only package name or restrict search strictly to package name
  -r, --rootdir <path>     root directory for isolated system operations
  -s, --search             search locally-installed packages for matching strings
  -v, --verbose            be verbose
      --list               list all installed packages
      --arch <arch>        filter by architecture
      --api-level <level>  filter by highest API level
      --api <level>        alias for --api-level
      --api-level-exact <level>
                           filter strictly to an exact Android API level
      --api-exact <level>  alias for --api-level-exact
`)
	case "remove":
		fmt.Print(`usage:  apexm {-R --remove} [options] <package(s)>
options:
  -c, --config <path>      set an alternate configuration file
  -i, --isolated           isolated mode: ignore system states (user) or operate on rootdir (system)
  -r, --rootdir <path>     root directory for isolated system operations
  -v, --verbose            be verbose
  -y, --noconfirm          do not ask for any confirmation
`)
	case "upgrade":
		fmt.Print(`usage:  apexm {-U --upgrade} [options] <file(s)>
options:
  -c, --config <path>      set an alternate configuration file
  -i, --isolated           isolated mode: ignore system states (user) or operate on rootdir (system)
  -r, --rootdir <path>     root directory for isolated system operations
  -v, --verbose            be verbose
  -y, --noconfirm          do not ask for any confirmation
`)
	case "files":
		fmt.Print(`usage:  apexm {-F --files} [options] [file(s)]
options:
  -c, --config <path>      set an alternate configuration file
  -v, --verbose            be verbose
`)
	case "init":
		fmt.Print(`usage:  apexm {--init} [options]
options:
  -c, --config <path>      set an alternate configuration file
  -v, --verbose            be verbose
`)
	default:
		fmt.Print(`usage:  apexm <operation> [...]
operations:
    apexm {-h --help}
    apexm {-V --version}
    apexm {-F --files}    [options] [file(s)]
    apexm {-Q --query}    [options] [package(s)]
    apexm {-R --remove}   [options] <package(s)>
    apexm {-S --sync}     [options] [package(s)]
    apexm {-U --upgrade}  [options] <file(s)>
    apexm {   --init}     [options]

use 'apexm {-h --help}' with an operation for available options
`)
	}
}

func printVersion() {
	fmt.Println()
	fmt.Println(" .---.                 apexm v1.0.0 - Android APEX Package Manager")
	fmt.Println("/     \\                Android Generic Project")
	fmt.Println("| () () |              Copyright (C) 2026 BlissLabs")
	fmt.Println(" \\  _  /")
	fmt.Println("  `---'")
}
