package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	overlayKernel = "overlayfs"
	overlayFUSE   = "fuse-overlayfs"
	overlayPlain  = "plain"
)

func makeOverlayDirs(base string) error {
	for _, name := range []string{"lower", "upper", "work", "merged"} {
		if err := os.MkdirAll(joinOverlay(base, name), 0o755); err != nil {
			return fmt.Errorf("overlay mkdir: %w", err)
		}
	}
	return nil
}

func mountOverlay(base string) (method string, err error) {
	lower := joinOverlay(base, "lower")
	upper := joinOverlay(base, "upper")
	work := joinOverlay(base, "work")
	merged := joinOverlay(base, "merged")
	if err := tryKernelOverlay(lower, upper, work, merged); err == nil {
		return overlayKernel, nil
	}
	if err := tryFuseOverlay(lower, upper, work, merged); err == nil {
		return overlayFUSE, nil
	}
	// fuse-overlayfs and kernel overlay both need the lower tree to
	// exist before mount. If neither mount works, bind the lower dir
	// itself. Export/import still work; there is no upper delta until
	// a later rsync-style pass.
	return overlayPlain, nil
}

func joinOverlay(base, name string) string {
	return base + string(os.PathSeparator) + name
}

func tryKernelOverlay(lower, upper, work, merged string) error {
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upper, work)
	cmd := exec.Command("mount", "-t", "overlay", "overlay", "-o", opts, merged)
	if out, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else if os.Getenv("SPIKE_SANDBOX_SUDO") == "1" {
		sudo := exec.Command("sudo", "-n", "mount", "-t", "overlay", "overlay", "-o", opts, merged)
		if out2, err2 := sudo.CombinedOutput(); err2 == nil {
			return nil
		} else {
			return fmt.Errorf("overlayfs: %s / sudo: %s", bytesHead(out, err), bytesHead(out2, err2))
		}
	} else {
		return fmt.Errorf("overlayfs: %s", bytesHead(out, err))
	}
}

func tryFuseOverlay(lower, upper, work, merged string) error {
	if _, err := exec.LookPath("fuse-overlayfs"); err != nil {
		return err
	}
	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s,allow_other", lower, upper, work)
	cmd := exec.Command("fuse-overlayfs", "-o", opts, merged)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fuse-overlayfs: %s", bytesHead(out, err))
	}
	return nil
}

func unmountOverlay(method, merged string) error {
	switch method {
	case overlayKernel:
		cmd := exec.Command("umount", merged)
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else if os.Getenv("SPIKE_SANDBOX_SUDO") == "1" {
			sudo := exec.Command("sudo", "-n", "umount", merged)
			if out2, err2 := sudo.CombinedOutput(); err2 != nil {
				return fmt.Errorf("umount overlay: %s / sudo: %s", bytesHead(out, err), bytesHead(out2, err2))
			}
			return nil
		} else {
			return fmt.Errorf("umount overlay: %s", bytesHead(out, err))
		}
	case overlayFUSE:
		unmount := "fusermount3"
		if _, err := exec.LookPath(unmount); err != nil {
			unmount = "fusermount"
		}
		cmd := exec.Command(unmount, "-u", merged)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %s", unmount, bytesHead(out, err))
		}
		return nil
	default:
		return nil
	}
}

func bytesHead(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if s == "" && err != nil {
		return err.Error()
	}
	if err != nil && s != "" {
		return err.Error() + ": " + s
	}
	return s
}

func currentUserSpec() string {
	uid := os.Getuid()
	gid := os.Getgid()
	if uid == 0 {
		return ""
	}
	return strconv.Itoa(uid) + ":" + strconv.Itoa(gid)
}
