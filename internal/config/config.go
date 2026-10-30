package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultAddr = "127.0.0.1:8080"
)

type Config struct {
	Addr        string
	DBPath      string
	AllowRemote bool
	ConfigPath  string
	ConfigFound bool
	UserName    string
	NameSet     bool
	// AI 是 OpenAI 兼容接口的三项配置；空串表示未设置。
	AI AI
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
	AI struct {
		BaseURL string   `yaml:"base_url"`
		APIKey  string   `yaml:"api_key"`
		Models  []string `yaml:"models"`
		Prompt  string   `yaml:"prompt"`
	} `yaml:"ai"`
}

func Load(args []string) (Config, []string, error) {
	defConfig, err := DefaultConfigFile()
	if err != nil {
		return Config{}, nil, err
	}
	defDB, err := DefaultDBFile()
	if err != nil {
		return Config{}, nil, err
	}
	cfg := Config{
		Addr:       DefaultAddr,
		DBPath:     defDB,
		ConfigPath: defConfig,
	}

	flags := flag.NewFlagSet("note", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", defConfig, "path to YAML config file")
	configShort := flags.String("c", defConfig, "shorthand for -config")
	addr := flags.String("addr", "", "listen address")
	port := flags.Int("port", 0, "listen port; overrides the port part of the address")
	portShort := flags.Int("p", 0, "shorthand for -port")
	dbPath := flags.String("db", "", "SQLite database path")
	allowRemote := flags.Bool("allow-remote", false, "allow binding to a non-loopback address")
	remoteShort := flags.Bool("remote", false, "shorthand for -allow-remote")
	if err := flags.Parse(args); err != nil {
		return Config{}, nil, err
	}
	if flags.NArg() > 0 {
		return Config{}, nil, fmt.Errorf("unexpected argument %q: note 不接受多余的位置参数", flags.Arg(0))
	}

	// -c 与 --config 同义：显式设置过哪个用哪个，都设置时以 --config 为准
	if visitContains(flags, "config", "c") {
		cfg.ConfigPath = *configPath
		if !visitContains(flags, "config") {
			cfg.ConfigPath = *configShort
		}
	}
	var warnings []string

	data, err := os.ReadFile(cfg.ConfigPath)
	switch {
	case err == nil:
		cfg.ConfigFound = true
		fileCfg, err := decode(data)
		if err != nil {
			return Config{}, nil, fmt.Errorf("parse config %s: %w", cfg.ConfigPath, err)
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
		cfg.AI = AI{
			BaseURL: fileCfg.AI.BaseURL,
			APIKey:  fileCfg.AI.APIKey,
			Models:  fileCfg.AI.Models,
			Prompt:  fileCfg.AI.Prompt,
		}
	case errors.Is(err, os.ErrNotExist):
		if visitContains(flags, "config", "c") {
			return Config{}, nil, fmt.Errorf("config file not found: %s", cfg.ConfigPath)
		}
		warnings = append(warnings, fmt.Sprintf("config file %s not found; using built-in defaults (pass -c to specify one)", *configPath))
	default:
		return Config{}, nil, fmt.Errorf("read config %s: %w", cfg.ConfigPath, err)
	}

	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "addr":
			cfg.Addr = *addr
		case "db":
			cfg.DBPath = *dbPath
		case "allow-remote", "remote":
			cfg.AllowRemote = *allowRemote || *remoteShort
		}
	})

	// --port / -p 只覆盖端口，host 沿用已生效的监听地址（内置默认、配置文件或 --addr）
	if visitContains(flags, "port", "p") {
		value := *port
		if value == 0 {
			value = *portShort
		}
		host, _, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return Config{}, nil, fmt.Errorf("invalid listen address %q: %w", cfg.Addr, err)
		}
		cfg.Addr = net.JoinHostPort(host, strconv.Itoa(value))
	}

	if err := validate(cfg); err != nil {
		return Config{}, nil, err
	}
	if cfg.AllowRemote {
		warnings = append(warnings, "remote binding enabled; put the server behind TLS and authentication")
	}

	return cfg, warnings, nil
}

// visitContains 判断 FlagSet 里显式设置过的 flag 是否包含 names 中的任一个。
func visitContains(flags *flag.FlagSet, names ...string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		for _, name := range names {
			if f.Name == name {
				found = true
			}
		}
	})
	return found
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
	if err := ValidateAI(cfg.AI); err != nil {
		return fmt.Errorf("parse config %s: %w", cfg.ConfigPath, err)
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

	return writeAtomic(c.ConfigPath, buf.Bytes())
}
