package configs

import (
	"encoding/json"
	"fmt"
	"os"
)

type TraderConfig struct {
	APIKey     string `json:"api_key"`
	APISecret  string `json:"api_secret"`
	Passphrase string `json:"passphrase"`
}

func LoadConfigs(filename string) ([]TraderConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var configs []TraderConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}

	fmt.Println(configs)

	return configs, nil
}
