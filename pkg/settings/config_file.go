package settings

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/ini.v1"
)

var configFileMutex sync.Mutex

// ConfigurationSectionManagedExternally reports deployment-owned environment overrides.
// Lock the whole editable section so saving cannot appear to override deployment policy.
func ConfigurationSectionManagedExternally(section string) bool {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if value != "" && (strings.HasPrefix(name, "EBK_"+strings.ToUpper(section)+"_") || strings.HasPrefix(name, "EBKCFP_"+strings.ToUpper(section)+"_")) {
			return true
		}
	}
	return false
}

// updateConfigurationFile serializes the entire read-modify-replace operation.
func updateConfigurationFile(path string, edit func(*ini.File) error) error {
	configFileMutex.Lock()
	defer configFileMutex.Unlock()
	file, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, path)
	if err != nil {
		return err
	}
	if err = edit(file); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ezbookkeeping-*.ini")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Close(); err != nil {
		return err
	}
	if err = file.SaveTo(temp.Name()); err != nil {
		return err
	}
	if err = os.Chmod(temp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
