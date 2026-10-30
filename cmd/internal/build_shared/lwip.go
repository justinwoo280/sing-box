package build_shared

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// PrepareLWIPWorkspace gives gomobile a real module, since gomobile creates its
// own intermediate modules and must not inherit an application's -modfile flag.
func PrepareLWIPWorkspace(directory string) (string, error) {
	modfile, err := PrepareLWIPSource(directory)
	if err != nil {
		return "", err
	}
	workspace := filepath.Join(filepath.Dir(modfile), "app")
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if filepath.Dir(relative) == "." {
				switch entry.Name() {
				case ".cache", ".zig-cache", "dist", "build", "bin", "site", "venv", "node_modules":
					return filepath.SkipDir
				}
			}
			if path == filepath.Dir(modfile) || strings.HasSuffix(entry.Name(), ".xcframework") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(workspace, relative), 0o755)
		}
		if entry.Name() == ".git" || strings.HasSuffix(entry.Name(), ".aar") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(workspace, relative), data, 0o644)
	})
	if err != nil {
		return "", err
	}
	for _, extension := range []string{"mod", "sum"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(modfile), "source."+extension))
		if err != nil {
			return "", err
		}
		if err = os.WriteFile(filepath.Join(workspace, "go."+extension), data, 0o644); err != nil {
			return "", err
		}
	}
	return workspace, nil
}
