package types

import (
	"os"
)

// ServerConfig contains cluster-wide SSH parameters.
// The bind addresses are not part of the configuration: they are defined by
// the listeners of the Gateway resources.
type ServerConfig struct {
	HostKeyFile string
	DebugImage  string
}

func GetServerConf() *ServerConfig {
	return &ServerConfig{
		HostKeyFile: getEnv("HOST_KEY_FILE", "/secret/ssh-privatekey"),
		DebugImage:  getEnv("DEBUG_IMAGE", "busybox"),
	}
}

func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}
