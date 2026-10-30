package build_shared

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AddLWIPAARNotices keeps the native notices with the distributed Android library.
// The same locked native source is used by both prebuilt and API-21 source builds.
func AddLWIPAARNotices(path string, sourceLock string, androidAPI int) error {
	command := exec.Command("go", "mod", "download", "-json", "github.com/justinwoo280/sing-lwip/lib/android_arm64")
	command.Stderr = os.Stderr
	data, err := command.Output()
	if err != nil {
		return err
	}
	var module struct{ Dir string }
	if err = json.Unmarshal(data, &module); err != nil {
		return err
	}
	if module.Dir == "" {
		return fmt.Errorf("pinned Android lwIP library has no directory")
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	output, err := os.CreateTemp(filepath.Dir(path), ".libbox-notices-*.aar")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	writer := zip.NewWriter(output)
	for _, file := range reader.File {
		if err = writer.Copy(file); err != nil {
			return err
		}
	}
	notices := map[string]string{
		"LICENSE":      filepath.Join(module.Dir, "LICENSE"),
		"LWIP-LICENSE": filepath.Join(module.Dir, "LWIP-LICENSE"),
	}
	mode := "prebuilt"
	if sourceLock == "" {
		notices["manifest.json"] = filepath.Join(module.Dir, "manifest.json")
	} else {
		mode = "source"
		notices["native-source.json"] = sourceLock
	}
	for _, name := range []string{"LICENSE", "LWIP-LICENSE", "manifest.json", "native-source.json"} {
		filename, present := notices[name]
		if !present {
			continue
		}
		data, err = os.ReadFile(filename)
		if err != nil {
			return err
		}
		entry, err := writer.Create("META-INF/licenses/sing-lwip/" + name)
		if err != nil {
			return err
		}
		if _, err = entry.Write(data); err != nil {
			return err
		}
	}
	entry, err := writer.Create("META-INF/licenses/sing-lwip/build.json")
	if err != nil {
		return err
	}
	if err = json.NewEncoder(entry).Encode(map[string]any{"binding": mode, "android_api": androidAPI}); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	if err = output.Chmod(0o644); err != nil {
		return err
	}
	if err = output.Close(); err != nil {
		return err
	}
	if err = reader.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), path)
}
