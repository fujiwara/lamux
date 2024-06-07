package lamux

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

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

func Run(ctx context.Context) error {
	cfg := &Config{}
	kong.Parse(cfg)

	l, err := NewLamux(cfg)
	if err != nil {
		return err
	}
	var mux = http.NewServeMux()
	mux.HandleFunc("/", l.handleProxy)

	addr := fmt.Sprintf(":%d", cfg.Port)
	ridge.Run(addr, "/", mux)
	return nil
}

func (l *Lamux) handleProxy(w http.ResponseWriter, r *http.Request) {
	alias, err := extractAlias(r, l.Config.DomainSuffix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, err := ridge.ToRequestV2(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	input := &lambda.InvokeInput{
		FunctionName: &l.Config.FunctionName,
		Payload:      b,
		Qualifier:    aws.String(alias),
	}
	ctx, cancel := context.WithTimeout(r.Context(), l.Config.UpstreamTimeout)
	defer cancel()
	start := time.Now()
	resp, err := l.lambdaClient.Invoke(ctx, input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	elapsed := time.Since(start)
	slog.Info("upstream", "duration", elapsed, "alias", alias, "status", resp.StatusCode)
	if resp.FunctionError != nil {
		http.Error(w, *resp.FunctionError, http.StatusInternalServerError)
		return
	}
	var res ridge.Response
	if err := json.Unmarshal(resp.Payload, &res); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, err := res.WriteTo(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else {
		slog.Info("response", "method", r.Method, "url", r.URL.String(), "status", res.StatusCode, "bytes", n)
	}
}

func extractAlias(r *http.Request, suffix string) (string, error) {
	var host string
	var err error
	if host = r.Header.Get("X-Forwarded-Host"); host == "" {
		host = r.Host
	}
	host, _, err = net.SplitHostPort(host)
	if err != nil {
		return "", fmt.Errorf("invalid host")
	}
	slog.Info("extractAlias", "host", host, "suffix", suffix)
	if !strings.HasSuffix(host, suffix) {
		return "", fmt.Errorf("invalid domain suffix")
	}
	alias := strings.TrimSuffix(host, "."+suffix)
	if !aliasRegexp.MatchString(alias) {
		return "", fmt.Errorf("invalid alias")
	}
	return alias, nil
}
