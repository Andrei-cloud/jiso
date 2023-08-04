package config

import (
	"flag"
	"sync"
)

type Config struct {
	host         string
	port         string
	specFileName string
	file         string
}

var (
	config     *Config
	configOnce sync.Once
)

func GetConfig() *Config {
	configOnce.Do(func() {
		config = &Config{}
	})
	return config
}

func (c *Config) Parse() error {
	host := flag.String("host", "", "Hostname to connect to")
	port := flag.String("port", "", "Port to connect to")
	specFileName := flag.String("spec-file", "", "path to customized specification file in JSON format")
	file := flag.String("file", "", "path to transaction file in JSON format")
	flag.Parse()

	c.host = *host
	c.port = *port
	c.specFileName = *specFileName
	c.file = *file

	return nil
}

func (c *Config) SetHost(host string) {
	if host == "" {
		return
	}
	c.host = host
}

func (c *Config) SetPort(port string) {
	if port == "" {
		return
	}
	c.port = port
}

func (c *Config) SetSpec(specFileName string) {
	if specFileName == "" {
		return
	}
	c.specFileName = specFileName
}

func (c *Config) SetFile(file string) {
	if file == "" {
		return
	}
	c.file = file
}

func (c *Config) GetHost() string {
	return c.host
}

func (c *Config) GetPort() string {
	return c.port
}

func (c *Config) GetSpec() string {
	return c.specFileName
}

func (c *Config) GetFile() string {
	return c.file
}
