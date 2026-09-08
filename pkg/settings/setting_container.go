package settings

import (
	"fmt"
	"sync"
)

// ConfigContainer contains the current setting config
type ConfigContainer struct {
	mutex   sync.RWMutex
	current *Config
}

// Initialize a config container singleton instance
var (
	Container = &ConfigContainer{}
)

// SetCurrentConfig sets the current config by a given config
func SetCurrentConfig(config *Config) {
	Container.SetCurrentConfig(config)
}

// SetCurrentConfig replaces the current immutable configuration.
func (c *ConfigContainer) SetCurrentConfig(config *Config) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.current = config
}

// GetCurrentConfig returns the current config
func (c *ConfigContainer) GetCurrentConfig() *Config {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.current
}

// UpdateEmailBillConfig replaces only the email bill configuration without mutating readers' snapshots.
func (c *ConfigContainer) UpdateEmailBillConfig(emailConfig *EmailBillConfig) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.current == nil {
		return fmt.Errorf("current configuration is not initialized")
	}

	updated := *c.current
	updated.EmailBillConfig = cloneEmailBillConfig(emailConfig)
	c.current = &updated
	return nil
}

func cloneEmailBillConfig(config *EmailBillConfig) *EmailBillConfig {
	if config == nil {
		return nil
	}

	cloned := *config
	cloned.TrustedAuthservDomains = append([]string(nil), config.TrustedAuthservDomains...)
	return &cloned
}
