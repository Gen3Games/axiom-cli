package app

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultAPIBaseURL  = "http://localhost:3000/api/cli"
	defaultEVMRPCURL   = "https://rpc.xrplevm.org"
	defaultXRPLRPCURL  = "https://s1.ripple.com:51234"
	keyringServiceName = "axiom-cli"
)

type Config struct {
	APIBaseURL    string             `json:"apiBaseUrl"`
	EVMRPCURL     string             `json:"evmRpcUrl"`
	XRPLRPCURL    string             `json:"xrplRpcUrl"`
	DeviceID      string             `json:"deviceId"`
	ActiveProfile string             `json:"activeProfile"`
	OutputFormat  string             `json:"outputFormat"`
	Profiles      map[string]Profile `json:"profiles"`
}

type Profile struct {
	Name                  string `json:"name"`
	EVMAddress            string `json:"evmAddress"`
	XRPLAddress           string `json:"xrplAddress,omitempty"`
	DepositDestinationTag int    `json:"depositDestinationTag,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		APIBaseURL:    valueOrDefault(os.Getenv("AXIOM_CLI_API_URL"), defaultAPIBaseURL),
		EVMRPCURL:     valueOrDefault(os.Getenv("AXIOM_CLI_RPC_URL"), defaultEVMRPCURL),
		XRPLRPCURL:    valueOrDefault(os.Getenv("AXIOM_CLI_XRPL_RPC_URL"), defaultXRPLRPCURL),
		ActiveProfile: "default",
		OutputFormat:  "text",
		Profiles: map[string]Profile{
			"default": {Name: "default"},
		},
	}
}

func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if _, err := ensureDeviceID(cfg); err != nil {
			return nil, err
		}
		if err := SaveConfig(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{"default": {Name: "default"}}
	}
	if cfg.ActiveProfile == "" {
		cfg.ActiveProfile = "default"
	}
	if generated, err := ensureDeviceID(cfg); err != nil {
		return nil, err
	} else if generated {
		if err := SaveConfig(cfg); err != nil {
			return nil, err
		}
	}
	if _, ok := cfg.Profiles[cfg.ActiveProfile]; !ok {
		cfg.Profiles[cfg.ActiveProfile] = Profile{Name: cfg.ActiveProfile}
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(dir, "axiom-cli", "config.json"), nil
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (c *Config) CurrentProfile() *Profile {
	profile, ok := c.Profiles[c.ActiveProfile]
	if !ok {
		profile = Profile{Name: c.ActiveProfile}
		c.Profiles[c.ActiveProfile] = profile
	}
	return &profile
}

func (c *Config) SetCurrentProfile(profile Profile) {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	c.Profiles[profile.Name] = profile
}

func EVMSecretKey(profileName string) string {
	return fmt.Sprintf("evm:%s", profileName)
}

func XRPLSecretKey(profileName string) string {
	return fmt.Sprintf("xrpl:%s", profileName)
}

func KeyringServiceName() string {
	return keyringServiceName
}

func generateDeviceID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	), nil
}

func ensureDeviceID(cfg *Config) (bool, error) {
	if cfg.DeviceID != "" {
		return false, nil
	}

	deviceID, err := generateDeviceID()
	if err != nil {
		return false, err
	}
	cfg.DeviceID = deviceID
	return true, nil
}
