package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateBackendPath(socket string) error {
	dir := filepath.Dir(socket)
	if filepath.Base(socket) != "ui.sock" || filepath.Dir(dir) != "/tmp" || !strings.HasPrefix(filepath.Base(dir), "rex-nvim-") {
		return fmt.Errorf("backend socket must be in a private rex-nvim temporary directory")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("backend directory must be a private directory, not a symlink")
	}
	return nil
}
