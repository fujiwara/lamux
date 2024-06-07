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

type Config struct {
	Port            int           `help:"Port to listen on" default:"8080" env:"LAMUX_PORT" name:"port"`
	FunctionName    string        `help:"Name of the Lambda function to proxy" required:"" env:"LAMUX_FUNCTION_NAME" name:"function-name"`
	DomainSuffix    string        `help:"Domain suffix to accept requests for" required:"" env:"LAMUX_DOMAIN_SUFFIX" name:"domain-suffix"`
	UpstreamTimeout time.Duration `help:"Timeout for upstream requests" default:"30s" env:"LAMUX_UPSTREAM_TIMEOUT" name:"upstream-timeout"`
}

type Lamux struct {
	Config       *Config
	awsCfg       aws.Config
	lambdaClient *lambda.Client
}

func NewLamux(cfg *Config) (*Lamux, error) {
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

var aliasRegexp = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

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
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
	alias, err := extractAlias(ctx, r, l.Config.DomainSuffix)
	if err != nil {
		slog.ErrorContext(ctx, "extractAlias", "error", err)
		return fmt.Errorf("invalid alias: %w", err)
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
		FunctionName: &l.Config.FunctionName,
		Payload:      b,
		Qualifier:    aws.String(alias),
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

func extractAlias(_ context.Context, r *http.Request, suffix string) (string, error) {
	var host string
	var err error
	if host = r.Header.Get("X-Forwarded-Host"); host == "" {
		host = r.Host
	}
	host, _, err = net.SplitHostPort(host)
	if err != nil {
		return "", fmt.Errorf("invalid host")
	}
	if !strings.HasSuffix(host, suffix) {
		return "", fmt.Errorf("invalid domain suffix")
	}
	alias := strings.TrimSuffix(host, "."+suffix)
	if !aliasRegexp.MatchString(alias) {
		return "", fmt.Errorf("invalid alias")
	}
	return alias, nil
}
