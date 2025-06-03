package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultCheckIntervalSeconds = 5 * 60
	DefaultComposeFile = "docker-compose.yml"
)

type RepositoryConfig struct {
	BasePath string `yaml:"basePath"`
	GitURL 	 string `yaml:"gitUrl"`
	CloneDirName string `yaml:"cloneDirName"`
	Branch string `yaml:"branch"`
	ServiceName string `yaml:"serviceName"`
	ComposeFile string `yaml:"composeFile"`
	CheckIntervalSeconds int `yaml:"checkIntervalSeconds"`
}

type AppConfig struct {
	Repositories []RepositoryConfig `yaml:"repositories"`
}

// LoadConfig reads the configuration file from the given YAML path
func LoadConfig(filePath string) (*AppConfig, error) {
	if (filePath == "") {
		return nil, fmt.Errorf("config file path cannot be empty")
	}

	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for config file '%s': %w", filePath, err)
	}

	data, err := os.ReadFile(absFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file '%s' not found. Please create it or specify a valid path with -config", absFilePath)
		}
			return nil, fmt.Errorf("failed to read config file '%s': %w", absFilePath, err)
	}

	var cfg AppConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
				return nil, fmt.Errorf("failed to unmarshal config data from '%s': %w", absFilePath, err)

	}

	// Validate and apply defaults
	for i := range cfg.Repositories {
		repo := &cfg.Repositories[i] // Get a pointer to modify the struct in the slice

		if repo.BasePath == "" {
			return nil, fmt.Errorf("repository config at index %d missing required 'basePath'", i)
		}
		if repo.GitURL == "" {
			return nil, fmt.Errorf("repository config for '%s/%s' missing required 'gitUrl'", repo.BasePath, repo.CloneDirName)
		}
		if repo.CloneDirName == "" {
			return nil, fmt.Errorf("repository config for gitUrl '%s' missing required 'cloneDirName'", repo.GitURL)
		}
		if repo.Branch == "" {
			return nil, fmt.Errorf("repository config for '%s/%s' missing required 'branch'", repo.BasePath, repo.CloneDirName)
		}
		if repo.ServiceName == "" {
			return nil, fmt.Errorf("repository config for '%s/%s' missing required 'serviceName'", repo.BasePath, repo.CloneDirName)
		}

		if repo.ComposeFile == "" {
			repo.ComposeFile = DefaultComposeFile
		}
		if repo.CheckIntervalSeconds <= 0 {
			repo.CheckIntervalSeconds = DefaultCheckIntervalSeconds
		}
	}

	return &cfg, nil
}