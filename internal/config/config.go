package config

import (
	"encoding/json"
	"log"
	"os"
)

type Config struct {
	Password string `json:"password"`
	Port     string `json:"port"`
}

var AppConfig *Config

func Load() error {
	file, err := os.Open("config.json")
	if err != nil {
		// Create default config if it doesn't exist
		if os.IsNotExist(err) {
			log.Println("Config file not found, creating default config.json")
			AppConfig = &Config{
				Password: "changeme",
				Port:     "8080",
			}
			return Save()
		}
		return err
	}
	defer file.Close()

	AppConfig = &Config{}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(AppConfig); err != nil {
		return err
	}

	log.Println("Configuration loaded successfully")
	return nil
}

func Save() error {
	file, err := os.Create("config.json")
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(AppConfig)
}
