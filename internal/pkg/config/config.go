package config

import "os"
import "gopkg.in/yaml.v2"

type RouterIDAssignmentConfig struct {
	ASNs []struct {
		ASN     string `yaml:"asn"`
		Network string `yaml:"network"`
	} `yaml:"asns"`
}

type Config struct {
	RouterIDAssignmentConfig RouterIDAssignmentConfig `yaml:"routerIdAssignment"`
}

func GetConfig() (*Config, error) {

	f, err := os.Open("config.yaml")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
