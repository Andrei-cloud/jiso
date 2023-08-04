package config

import (
	"flag"
)

type Config struct {
	Host         string
	Port         string
	SpecFileName string
	File         string
}

func Parse() (*Config, error) {
	host := flag.String("host", "", "Hostname to connect to")
	port := flag.String("port", "", "Port to connect to")
	specFileName := flag.String("spec-file", "", "path to customized specification file in JSON format")
	file := flag.String("file", "", "path to transaction file in JSON format")
	flag.Parse()

	// if *specFileName == "" || *file == "" {
	// 	return nil, fmt.Errorf("spec-file and file are required arguments")
	// }

	return &Config{
		Host:         *host,
		Port:         *port,
		SpecFileName: *specFileName,
		File:         *file,
	}, nil
}
