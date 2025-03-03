package config

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

var (
	defaultPluginConfig = make(map[string]Configuration)
)

// Configuration is the interface that enable the implementation to parse config from the global config file.
// Plugin admin and prometheus are two examples.
type Configuration interface {
	// Validate validates the configuration.
	// If returns error, the broker will not start.
	Validate() error
	// Unmarshaler defined how to unmarshal YAML into the config structure.
	yaml.Unmarshaler
}

// RegisterDefaultPluginConfig registers the default configuration for the given plugin.
func RegisterDefaultPluginConfig(name string, config Configuration) {
	if _, ok := defaultPluginConfig[name]; ok {
		panic(fmt.Sprintf("duplicated default config for %s plugin", name))
	}
	defaultPluginConfig[name] = config

}

// DefaultConfig return the default configuration.
// If config file is not provided, gmqttd will start with DefaultConfig.
func DefaultConfig() Config {
	c := Config{
		Listeners: DefaultListeners,
		MQTT:      DefaultMQTTConfig,
		API:       DefaultAPI,
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		Plugins:           make(pluginConfig),
		Persistence:       DefaultPersistenceConfig,
		TopicAliasManager: DefaultTopicAliasManager,
	}

	for name, v := range defaultPluginConfig {
		c.Plugins[name] = v
	}
	return c
}

var DefaultListeners = []*ListenerConfig{
	{
		Address:    "0.0.0.0:1883",
		TLSOptions: nil,
		Websocket:  nil,
	},
	{
		Address: "0.0.0.0:8883",
		Websocket: &WebsocketOptions{
			Path: "/",
		},
	},
}

// LogConfig is use to configure the log behaviors.
type LogConfig struct {
	// Level is the log level. Possible values: debug, info, warn, error
	Level string `yaml:"level"`
	// Format is the log format. Possible values: json, text
	Format string `yaml:"format"`
	// DumpPacket indicates whether to dump MQTT packet in debug level.
	DumpPacket bool `yaml:"dump_packet"`
}

func (l LogConfig) Validate() error {
	if l.Level != "debug" && l.Level != "info" && l.Level != "warn" && l.Level != "error" {
		return fmt.Errorf("invalid log level: %s", l.Level)
	}
	if l.Format != "json" && l.Format != "text" {
		return fmt.Errorf("invalid log format: %s", l.Format)
	}
	return nil
}

// pluginConfig stores the plugin default configuration, key by the plugin name.
// If the plugin has default configuration, it should call RegisterDefaultPluginConfig in it's init function to register.
type pluginConfig map[string]Configuration

func (p pluginConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	for _, v := range p {
		err := unmarshal(v)
		if err != nil {
			return err
		}
	}
	return nil
}

// Config is the configration for gmqttd.
type Config struct {
	Listeners []*ListenerConfig `yaml:"listeners"`
	API       API               `yaml:"api"`
	MQTT      MQTT              `yaml:"mqtt,omitempty"`
	GRPC      GRPC              `yaml:"gRPC"`
	Log       LogConfig         `yaml:"log"`
	PidFile   string            `yaml:"pid_file"`
	ConfigDir string            `yaml:"config_dir"`
	Plugins   pluginConfig      `yaml:"plugins"`
	// PluginOrder is a slice that contains the name of the plugin which will be loaded.
	// Giving a correct order to the slice is significant,
	// because it represents the loading order which affect the behavior of the broker.
	PluginOrder       []string          `yaml:"plugin_order"`
	Persistence       Persistence       `yaml:"persistence"`
	TopicAliasManager TopicAliasManager `yaml:"topic_alias_manager"`
}

type GRPC struct {
	Endpoint string `yaml:"endpoint"`
}

type TLSOptions struct {
	// CACert is the trust CA certificate file.
	CACert string `yaml:"cacert"`
	// Cert is the path to certificate file.
	Cert string `yaml:"cert"`
	// Key is the path to key file.
	Key string `yaml:"key"`
	// Verify indicates whether to verify client cert.
	Verify bool `yaml:"verify"`
}

type ListenerConfig struct {
	Address     string `yaml:"address"`
	*TLSOptions `yaml:"tls"`
	Websocket   *WebsocketOptions `yaml:"websocket"`
}

type WebsocketOptions struct {
	Path string `yaml:"path"`
}

func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type config Config
	raw := config(DefaultConfig())
	if err := unmarshal(&raw); err != nil {
		return err
	}
	emptyMQTT := MQTT{}
	if raw.MQTT == emptyMQTT {
		raw.MQTT = DefaultMQTTConfig
	}
	if len(raw.Plugins) == 0 {
		raw.Plugins = make(pluginConfig)
		for name, v := range defaultPluginConfig {
			raw.Plugins[name] = v
		}
	} else {
		for name, v := range raw.Plugins {
			if v == nil {
				raw.Plugins[name] = defaultPluginConfig[name]
			}
		}
	}
	*c = Config(raw)
	return nil
}

func (c Config) Validate() (err error) {
	err = c.Log.Validate()
	if err != nil {
		return err
	}
	err = c.API.Validate()
	if err != nil {
		return err
	}
	err = c.MQTT.Validate()
	if err != nil {
		return err
	}
	err = c.Persistence.Validate()
	if err != nil {
		return err
	}
	for _, conf := range c.Plugins {
		err := conf.Validate()
		if err != nil {
			return err
		}
	}
	return nil
}

func ParseConfig(filePath string) (c Config, err error) {
	if filePath == "" {
		return DefaultConfig(), nil
	}
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return c, err
	}
	c = DefaultConfig()
	err = yaml.Unmarshal(b, &c)
	if err != nil {
		return c, err
	}
	c.ConfigDir = path.Dir(filePath)
	err = c.Validate()
	if err != nil {
		return Config{}, err
	}
	return c, err
}

func (c Config) GetLogger(config LogConfig) (l *zap.Logger, err error) {
	var logLevel zapcore.Level
	err = logLevel.UnmarshalText([]byte(config.Level))
	if err != nil {
		return
	}
	warnIoWriter := getWriter("./logs/%Y-%m/gmqtt.log")
	_ = os.Mkdir("./logs", 0755)
	// var writer = getLogWriter()
	var encoder = zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	if config.Format == "json" {
		encoder = zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	}
	// if config.Format == "text" {
	// 	core = zapcore.NewCore(zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()), writer, logLevel)
	// }
	var coreFile = zapcore.NewCore(encoder, zapcore.AddSync(warnIoWriter), logLevel)
	var coreConsole = zapcore.NewCore(encoder, os.Stdout, logLevel)

	var core = zapcore.NewTee(coreFile, coreConsole)
	zaplog := zap.New(core, zap.AddStacktrace(zap.ErrorLevel), zap.AddCaller())
	return zaplog, nil
}

// func getLogWriter() zapcore.WriteSyncer {
// 	lumberJackLogger := &lumberjack.Logger{
// 		Filename:   "./log/test.log",
// 		MaxSize:    1,
// 		MaxBackups: 5,
// 		MaxAge:     30,
// 		Compress:   false,
// 	}
// 	return zapcore.AddSync(lumberJackLogger)
// }

// 日志文件切割
func getWriter(logFile string) io.Writer {
	// 保存30天内的日志，每24小时(整点)分割一次日志
	writer, err := rotatelogs.New(
		logFile+".%Y%m%d",                             //每天
		rotatelogs.WithLinkName("./logs/current.log"), //生成软链，指向最新日志文件
		rotatelogs.WithRotationTime(24*time.Hour),     //最小为1分钟轮询。默认60s  低于1分钟就按1分钟来
		rotatelogs.WithRotationCount(0),               //设置3份 大于3份 或到了清理时间 开始清理 0不启用
		rotatelogs.WithMaxAge(30*24*time.Hour),        //保留30天日志
		rotatelogs.WithRotationSize(100*1024*1024),    //设置100MB大小,当大于这个容量时，创建新的日志文件
	)

	if err != nil {
		panic(err)
	}
	return writer
}
