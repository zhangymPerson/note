package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "./config/note.yaml"
	DefaultAddr       = "127.0.0.1:8080"
	DefaultDBPath     = "./data/notes.db"
)

type Config struct {
	Addr        string
	DBPath      string
	AllowRemote bool
	ConfigPath  string
	UserName    string
	NameSet     bool
}

type fileConfig struct {
	Server struct {
		Addr        string `yaml:"addr"`
		AllowRemote bool   `yaml:"allow_remote"`
	} `yaml:"server"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	User struct {
		Name    string `yaml:"name"`
		NameSet bool   `yaml:"name_set"`
	} `yaml:"user"`
}

func Load(args []string) (Config, []string, error) {
	cfg := Config{
		Addr:       DefaultAddr,
		DBPath:     DefaultDBPath,
		ConfigPath: DefaultConfigPath,
	}

	flags := flag.NewFlagSet("note", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", DefaultConfigPath, "path to YAML config file")
	addr := flags.String("addr", "", "listen address")
	dbPath := flags.String("db", "", "SQLite database path")
	allowRemote := flags.Bool("allow-remote", false, "allow binding to a non-loopback address")
	if err := flags.Parse(args); err != nil {
		return Config{}, nil, err
	}

	cfg.ConfigPath = *configPath
	var warnings []string

	data, err := os.ReadFile(*configPath)
	switch {
	case err == nil:
		fileCfg, err := decode(data)
		if err != nil {
			return Config{}, nil, fmt.Errorf("parse config %s: %w", *configPath, err)
		}
		if fileCfg.Server.Addr != "" {
			cfg.Addr = fileCfg.Server.Addr
		}
		cfg.AllowRemote = fileCfg.Server.AllowRemote
		if fileCfg.Database.Path != "" {
			cfg.DBPath = fileCfg.Database.Path
		}
		cfg.UserName = fileCfg.User.Name
		cfg.NameSet = fileCfg.User.NameSet
	case errors.Is(err, os.ErrNotExist):
		explicit := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "config" {
				explicit = true
			}
		})
		if explicit {
			return Config{}, nil, fmt.Errorf("config file not found: %s", *configPath)
		}
		warnings = append(warnings, fmt.Sprintf("config file %s not found; using built-in defaults", *configPath))
	default:
		return Config{}, nil, fmt.Errorf("read config %s: %w", *configPath, err)
	}

	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "addr":
			cfg.Addr = *addr
		case "db":
			cfg.DBPath = *dbPath
		case "allow-remote":
			cfg.AllowRemote = *allowRemote
		}
	})

	if err := validate(cfg); err != nil {
		return Config{}, nil, err
	}
	if cfg.AllowRemote {
		warnings = append(warnings, "remote binding enabled; put the server behind TLS and authentication")
	}

	return cfg, warnings, nil
}

func decode(data []byte) (fileConfig, error) {
	var cfg fileConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil
		}
		return fileConfig{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if strings.TrimSpace(cfg.Addr) == "" {
		return fmt.Errorf("listen address is empty")
	}

	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", cfg.Addr, err)
	}
	if host == "" {
		return fmt.Errorf("listen address %q must include a host", cfg.Addr)
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("invalid port in listen address %q", cfg.Addr)
	}
	if !cfg.AllowRemote && !isLoopbackHost(host) {
		return fmt.Errorf("listen address %q is not loopback; use --allow-remote to allow it", cfg.Addr)
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		return fmt.Errorf("database path is empty")
	}

	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// SaveUserName 把用户姓名写回配置文件并标记 name_set 为 true。
// 采用同目录临时文件 + rename 的原子替换，避免中断留下半截 YAML。
func (c Config) SaveUserName(name string) error {
	var out fileConfig
	out.Server.Addr = c.Addr
	out.Server.AllowRemote = c.AllowRemote
	out.Database.Path = c.DBPath
	out.User.Name = name
	out.User.NameSet = true

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(out); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	dir := filepath.Dir(c.ConfigPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".note-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, c.ConfigPath); err != nil {
		return fmt.Errorf("replace config %s: %w", c.ConfigPath, err)
	}
	return nil
}
