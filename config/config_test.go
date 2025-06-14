package config

import (
	"testing"
)

func TestLoadConfigEmptyPath(t *testing.T) {
	config, err := LoadConfig("")

	if config != nil {
		t.Errorf("config must be nil for empty path")
	}

	if err == nil {
		t.Errorf("error must not be nil for empty path")
	}
}