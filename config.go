package lamux

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var aliasRegexp = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
var functionNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

type Config struct {
	Port            int           `help:"Port to listen on" default:"8080" env:"LAMUX_PORT" name:"port"`
	FunctionName    string        `help:"Name of the Lambda function to proxy" default:"*" env:"LAMUX_FUNCTION_NAME" name:"function-name"`
	DomainSuffix    string        `help:"Domain suffix to accept requests for" default:"localdomain" env:"LAMUX_DOMAIN_SUFFIX" name:"domain-suffix"`
	UpstreamTimeout time.Duration `help:"Timeout for upstream requests" default:"30s" env:"LAMUX_UPSTREAM_TIMEOUT" name:"upstream-timeout"`
	Version         bool          `help:"Show version information" name:"version"`

	TraceConfig
}

type TraceConfig struct {
	TraceEndpoint string            `help:"Otel trace endpoint (e.g. localhost:4318)" env:"LAMUX_TRACE_ENDPOINT" name:"trace-endpoint"`
	TraceInsecure bool              `help:"Disable TLS for Otel trace endpoint" env:"LAMUX_TRACE_INSECURE" name:"trace-insecure"`
	TraceProtocol string            `help:"Otel trace protocol" env:"LAMUX_TRACE_PROTOCOL" name:"trace-protocol" default:"http" enum:"http,grpc"`
	TraceService  string            `help:"Otel trace service name" env:"LAMUX_TRACE_SERVICE" name:"trace-service" default:"lamux"`
	TraceHeaders  map[string]string `help:"Additional headers for Otel trace endpoint (key1=value1;key2=value2)" env:"LAMUX_TRACE_HEADERS" name:"trace-headers"`

	enableTrace bool
}

func (cfg *Config) Validate() error {
	if cfg.Port < 0 {
		return fmt.Errorf("port must not be negative")
	}
	if cfg.FunctionName == "" {
		return fmt.Errorf("function name must be set")
	}
	if cfg.FunctionName != "*" && !functionNameRegexp.MatchString(cfg.FunctionName) {
		return fmt.Errorf("invalid function name (%s allowed)", functionNameRegexp.String())
	}
	if cfg.DomainSuffix == "" {
		return fmt.Errorf("domain suffix must be set")
	}
	if cfg.UpstreamTimeout <= 0 {
		return fmt.Errorf("upstream timeout must be greater than 0")
	}

	if cfg.TraceConfig.TraceEndpoint != "" {
		cfg.TraceConfig.enableTrace = true
	}
	return nil
}

func (cfg *Config) ExtractAliasAndFunctionName(_ context.Context, r *http.Request) (string, string, error) {
	var host string
	if host = r.Header.Get("X-Forwarded-Host"); host == "" {
		host = r.Host
	}
	if raw, _, err := net.SplitHostPort(host); err == nil {
		host = raw
	}
	if !strings.HasSuffix(host, cfg.DomainSuffix) {
		return "", "", fmt.Errorf("invalid domain suffix (must be %s)", cfg.DomainSuffix)
	}

	if cfg.FunctionName != "*" { // fixed function name
		alias := strings.TrimSuffix(host, "."+cfg.DomainSuffix)
		if !aliasRegexp.MatchString(alias) {
			return "", "", fmt.Errorf("invalid alias (%s allowed)", aliasRegexp.String())
		}
		return alias, cfg.FunctionName, nil
	}

	// extract alias and function name from host
	target := strings.TrimSuffix(host, "."+cfg.DomainSuffix)
	p := strings.SplitN(target, "-", 2)
	if len(p) != 2 {
		return "", "", fmt.Errorf("invalid host name format. must be {alias}-{function}.%s", cfg.DomainSuffix)
	}
	alias, functionName := p[0], p[1]
	if !aliasRegexp.MatchString(alias) {
		return "", "", fmt.Errorf("invalid alias (%s allowed)", aliasRegexp.String())
	}
	if !functionNameRegexp.MatchString(functionName) {
		return "", "", fmt.Errorf("invalid function name (%s allowed)", functionNameRegexp.String())
	}
	return alias, functionName, nil
}
