package build_shared

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// PrepareLWIPSource creates a writable, verified source-CGO dependency and an
// alternate modfile. The application's dependency files and module cache stay
// immutable. Callers own directory and remove it after the build.
func PrepareLWIPSource(directory string) (string, error) {
	const module = "github.com/justinwoo280/sing-lwip"
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command("go", args...)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "GOWORK=off", "GOFLAGS=")
		cmd.Stderr = os.Stderr
		return cmd
	}
	data, err := command("mod", "download", "-json", module).Output()
	if err != nil {
		return "", fmt.Errorf("download pinned sing-lwip: %w", err)
	}
	var dependency struct{ Dir string }
	if err = json.Unmarshal(data, &dependency); err != nil {
		return "", err
	}
	if dependency.Dir == "" {
		return "", fmt.Errorf("pinned sing-lwip module has no source directory")
	}
	source := filepath.Join(directory, "source")
	if err = os.CopyFS(source, os.DirFS(dependency.Dir)); err != nil {
		return "", err
	}
	prepare := command("-C", source, "run", "./cmd/build-lwip", "sync-lwip")
	prepare.Stdout = os.Stderr
	if err = prepare.Run(); err != nil {
		return "", fmt.Errorf("prepare pinned lwIP sources: %w", err)
	}
	for _, extension := range []string{"mod", "sum"} {
		data, err = os.ReadFile("go." + extension)
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(filepath.Join(directory, "source."+extension), data, 0o600); err != nil {
			return "", err
		}
	}
	modfile := filepath.Join(directory, "source.mod")
	err = command("mod", "edit", "-modfile="+modfile, "-replace="+module+"="+source).Run()
	return modfile, err
}
