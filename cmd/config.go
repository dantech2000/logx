package cmd

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/dantech2000/logx/internal/logging"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v2"
)

// fileConfig is the on-disk configuration. Every field is optional: it provides
// defaults that an explicit flag overrides, plus custom field-name mappings that
// teach the parser a team's bespoke log schema.
type fileConfig struct {
	Level  string `yaml:"level"`
	Theme  string `yaml:"theme"`
	Color  string `yaml:"color"`
	Fields struct {
		Level     []string `yaml:"level"`
		Message   []string `yaml:"message"`
		Timestamp []string `yaml:"timestamp"`
	} `yaml:"fields"`
}

// loadedConfig holds the config for this run. Its zero value (no file present)
// is inert, so commands behave exactly as before when there is no config.
var loadedConfig fileConfig

// configPath resolves the config file location: $LOGX_CONFIG, else
// $XDG_CONFIG_HOME/logx/config.yaml, else ~/.config/logx/config.yaml.
func configPath() string {
	if p := os.Getenv("LOGX_CONFIG"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "logx", "config.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "logx", "config.yaml")
	}
	return ""
}

// loadConfig reads and parses the config file. A missing file is not an error
// (an empty config is returned); a malformed file is, so a typo surfaces.
func loadConfig() (fileConfig, error) {
	var cfg fileConfig
	path := configPath()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config %s: %w", path, err)
	}
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return cfg, nil
}

// applyFieldAliases teaches the parser any custom field names from the config.
func (c fileConfig) applyFieldAliases() {
	if len(c.Fields.Level)+len(c.Fields.Message)+len(c.Fields.Timestamp) == 0 {
		return
	}
	logging.RegisterFieldAliases(c.Fields.Level, c.Fields.Message, c.Fields.Timestamp)
}

// stringDefault returns the config value when the flag was not explicitly set
// and the config provides one, implementing flag > config > built-in precedence.
func stringDefault(flagValue, configValue string, changed bool) string {
	if changed {
		return flagValue
	}
	return cmp.Or(configValue, flagValue)
}

// effectiveLevel resolves the level string with flag > config > built-in default.
func effectiveLevel(cmd *cobra.Command) (string, error) {
	level, err := getStringFlag(cmd, flagLevel)
	if err != nil {
		return "", err
	}
	return stringDefault(level, loadedConfig.Level, cmd.Flags().Changed(flagLevel)), nil
}

// effectiveLogLevel resolves and parses the --level value in one step, so the
// commands that share the flag also share the resolution and error wording.
func effectiveLogLevel(cmd *cobra.Command) (logging.LogLevel, error) {
	levelStr, err := effectiveLevel(cmd)
	if err != nil {
		return logging.DEBUG, err
	}
	level, err := logging.ParseLogLevel(levelStr)
	if err != nil {
		return logging.DEBUG, fmt.Errorf("invalid level %q: %w", levelStr, err)
	}
	return level, nil
}
