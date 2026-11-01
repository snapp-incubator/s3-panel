package config

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/tidwall/pretty"
)

const (
	// prefix indicates environment variables prefix.
	prefix = "s3panel_"
)

// Config holds all configurations.
type Config struct {
	Logger        LoggerConfig        `json:"logger,omitempty"      koanf:"logger"`
	Server        ServerConfig        `json:"server,omitempty"      koanf:"server"`
	Cors          ServerCorsConfig    `json:"server_cors_config"    koanf:"server_cors_config"`
	ObjectStorage ObjectStorageConfig `json:"object_storage_config" koanf:"object_storage_config"`
	OIDC          OIDCConfig          `json:"oidc,omitempty"        koanf:"oidc"`
	Panel         PanelConfig         `json:"panel"                 koanf:"panel"`
}

func Provide(configPath string) Config {
	log.Printf("reading config from: %s", configPath)
	k := koanf.New(".")

	// load default configuration from default function
	if err := k.Load(structs.Provider(DefaultConfig(), "koanf"), nil); err != nil {
		log.Fatalf("error loading default: %s", err)
	}

	// load configuration from file
	if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
		log.Printf("error loading %s: %s", configPath, err)
	}

	// load environment variables
	if err := k.Load(
		// replace __ with . in environment variables so you can reference field A in struct B
		// as A__B.
		env.Provider(prefix, ".", func(source string) string {
			base := strings.ToLower(strings.TrimPrefix(source, prefix))

			return strings.ReplaceAll(base, "__", ".")
		}),
		nil,
	); err != nil {
		log.Printf("error loading environment variables: %s", err)
	}

	var instance Config
	if err := k.Unmarshal("", &instance); err != nil {
		log.Fatalf("error unmarshalling config: %s", err)
	}

	indent, err := json.MarshalIndent(redacted(instance), "", "\t")
	if err != nil {
		panic(err)
	}

	indent = pretty.Color(indent, nil)

	log.Printf(`
================ Loaded Configuration ================
%s
======================================================
	`, string(indent))

	return instance
}

// redacted returns a copy of the configuration that is safe to log.
//
// The loaded configuration is printed in full at startup, and it holds real
// secrets: the OIDC client secret (in iam mode this can be IAM's own Keycloak
// client), the session-cookie key, the STS client key that keeps /sts closed to
// everything but this console, the static bearer token and the S3 admin
// keypair. Pod logs are readable by everyone with access to the namespace and
// are usually shipped to a log store, so each of these is replaced with a marker
// that still says whether it was set.
func redacted(c Config) Config {
	mask := func(v string) string {
		if v == "" {
			return ""
		}
		return "[redacted]"
	}
	c.Server.AuthToken = mask(c.Server.AuthToken)
	c.ObjectStorage.AccessKeyAdmin = mask(c.ObjectStorage.AccessKeyAdmin)
	c.ObjectStorage.SecretKeyAdmin = mask(c.ObjectStorage.SecretKeyAdmin)
	c.ObjectStorage.STSClientKey = mask(c.ObjectStorage.STSClientKey)
	c.OIDC.ClientSecret = mask(c.OIDC.ClientSecret)
	c.OIDC.CookieKey = mask(c.OIDC.CookieKey)
	c.Panel.APIKey = mask(c.Panel.APIKey)
	return c
}
