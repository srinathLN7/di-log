package config

import (
	"os"
	"path/filepath"
)

var (
	CAFile         = configFile("ca.pem")
	ServerCertFile = configFile("server.pem")
	ServerKeyFile  = configFile("server-key.pem")
	ClientCertFile = configFile("client.pem")
	ClientKeyFile  = configFile("client-key.pem")
)

func configFile(filename string) string {

	// if CONFIG_DIR is explicitly specified in the env variables
	if dir := os.Getenv("CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, filename)
	}

	// default route to get the certs
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	return filepath.Join(homeDir, ".proglog", filename)
}
