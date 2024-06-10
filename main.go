package lamux

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	slogcontext "github.com/PumpkinSeed/slog-context"
	"github.com/alecthomas/kong"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/fujiwara/ridge"
)

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

type Config struct {
	Port            int           `help:"Port to listen on" default:"8080" env:"LAMUX_PORT" name:"port"`
	FunctionName    string        `help:"Name of the Lambda function to proxy" required:"" env:"LAMUX_FUNCTION_NAME" name:"function-name"`
	DomainSuffix    string        `help:"Domain suffix to accept requests for" required:"" env:"LAMUX_DOMAIN_SUFFIX" name:"domain-suffix"`
	UpstreamTimeout time.Duration `help:"Timeout for upstream requests" default:"30s" env:"LAMUX_UPSTREAM_TIMEOUT" name:"upstream-timeout"`
}

func (cfg *Config) Validate() error {
	if cfg.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}
	if cfg.FunctionName == "" {
		return fmt.Errorf("function name must be set")
	}
	if cfg.FunctionName != "*" && !nameRegexp.MatchString(cfg.FunctionName) {
		return fmt.Errorf("invalid function name (%s allowed)", nameRegexp.String())
	}
	if cfg.DomainSuffix == "" {
		return fmt.Errorf("domain suffix must be set")
	}
	if cfg.UpstreamTimeout <= 0 {
		return fmt.Errorf("upstream timeout must be greater than 0")
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
		if !nameRegexp.MatchString(alias) {
			return "", "", fmt.Errorf("invalid alias (%s allowed)", nameRegexp.String())
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
	if !nameRegexp.MatchString(alias) || !nameRegexp.MatchString(functionName) {
		return "", "", fmt.Errorf("invalid alias or function name (%s allowed)", nameRegexp.String())
	}
	return alias, functionName, nil
}

type Lamux struct {
	Config       *Config
	awsCfg       aws.Config
	lambdaClient *lambda.Client
}

func NewLamux(cfg *Config) (*Lamux, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Lamux{
		Config:       cfg,
		awsCfg:       awsCfg,
		lambdaClient: lambda.NewFromConfig(awsCfg),
	}, nil
}

type handlerFunc func(ctx context.Context, w http.ResponseWriter, r *http.Request) error

func Run(ctx context.Context) error {
	slog.SetDefault(slog.New(slogcontext.NewHandler(slog.NewJSONHandler(os.Stdout, nil))))

	cfg := &Config{}
	kong.Parse(cfg)

	l, err := NewLamux(cfg)
	if err != nil {
		return err
	}
	var mux = http.NewServeMux()
	mux.HandleFunc("/", l.wrapHandler(l.handleProxy))

	addr := fmt.Sprintf(":%d", cfg.Port)
	ridge.Run(addr, "/", mux)
	return nil
}

func (l *Lamux) wrapHandler(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), l.Config.UpstreamTimeout)
		defer cancel()
		ctx = setRequestContext(ctx, r)
		start := time.Now()
		err := h(ctx, w, r)
		elapsed := time.Since(start)
		ctx = slogcontext.WithValue(ctx, "duration", elapsed.Seconds())
		if err != nil {
			slog.InfoContext(ctx, "request", "status", http.StatusInternalServerError, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.InfoContext(ctx, "request", "status", http.StatusOK)
	}
}

func setRequestContext(ctx context.Context, r *http.Request) context.Context {
	ctx = slogcontext.WithValue(ctx, "remote", r.RemoteAddr)
	ctx = slogcontext.WithValue(ctx, "method", r.Method)
	ctx = slogcontext.WithValue(ctx, "url", r.URL.String())
	ctx = slogcontext.WithValue(ctx, "host", r.Host)
	ctx = slogcontext.WithValue(ctx, "user-agent", r.UserAgent())
	ctx = slogcontext.WithValue(ctx, "referer", r.Referer())
	ctx = slogcontext.WithValue(ctx, "x-forwarded-for", r.Header.Get("X-Forwarded-For"))
	ctx = slogcontext.WithValue(ctx, "x-forwarded-host", r.Header.Get("X-Forwarded-Host"))
	return ctx
}

func (l *Lamux) handleProxy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	alias, functionName, err := l.Config.ExtractAliasAndFunctionName(ctx, r)
	if err != nil {
		slog.ErrorContext(ctx, "handleProxy", "error", err)
		return err
	}

	payload, err := ridge.ToRequestV2(r)
	if err != nil {
		return fmt.Errorf("failed to convert request: %w", err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	input := &lambda.InvokeInput{
		FunctionName: aws.String(functionName),
		Qualifier:    aws.String(alias),
		Payload:      b,
	}
	resp, err := l.lambdaClient.Invoke(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to invoke: %w", err)
	}

	if resp.FunctionError != nil {
		return fmt.Errorf("function error: %s", *resp.FunctionError)
	}

	var res ridge.Response
	if err := json.Unmarshal(resp.Payload, &res); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if _, err := res.WriteTo(w); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}
	return nil
}
