package lamux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	slogcontext "github.com/PumpkinSeed/slog-context"
	"github.com/alecthomas/kong"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
	"github.com/fujiwara/ridge"
)

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

type handlerError struct {
	err  error
	code int
}

func (h *handlerError) Error() string {
	return h.err.Error()
}

func newHandlerError(err error, code int) error {
	return &handlerError{err: err, code: code}
}

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
	ridge.RunWithContext(ctx, addr, "/", mux)
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
			var herr *handlerError
			var code int
			if errors.As(err, &herr) {
				slog.ErrorContext(ctx, "request", "status", herr.code, "error", herr.err)
				code = herr.code
			} else {
				slog.ErrorContext(ctx, "request", "status", http.StatusInternalServerError, "error", err)
				code = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), code)
			return
		}
		slog.InfoContext(ctx, "response", "status", http.StatusOK)
	}
}

func setRequestContext(ctx context.Context, r *http.Request) context.Context {
	ctx = slogcontext.WithValue(ctx, "remote", r.RemoteAddr)
	ctx = slogcontext.WithValue(ctx, "method", r.Method)
	ctx = slogcontext.WithValue(ctx, "url", r.URL.String())
	ctx = slogcontext.WithValue(ctx, "host", r.Host)
	ctx = slogcontext.WithValue(ctx, "ua", r.UserAgent())
	ctx = slogcontext.WithValue(ctx, "referer", r.Referer())
	ctx = slogcontext.WithValue(ctx, "x_forwarded_for", r.Header.Get("X-Forwarded-For"))
	ctx = slogcontext.WithValue(ctx, "x_forwarded_host", r.Header.Get("X-Forwarded-Host"))
	if id := r.Header.Get("X-Amzn-Trace-Id"); id != "" {
		ctx = slogcontext.WithValue(ctx, "x_amzn_trace_id", id)
	}
	if id := r.Header.Get("X-Amz-Cf-Id"); id != "" {
		ctx = slogcontext.WithValue(ctx, "x_amz_cf_id", id)
	}
	if id := r.Header.Get("X-Amzn-RequestId"); id != "" {
		ctx = slogcontext.WithValue(ctx, "request_id", id)
	}
	return ctx
}

func (l *Lamux) handleProxy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	alias, functionName, err := l.Config.ExtractAliasAndFunctionName(ctx, r)
	if err != nil {
		err = newHandlerError(err, http.StatusBadRequest)
		slog.ErrorContext(ctx, "handleProxy", "error", err)
		return err
	}
	// prevent recursive call
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") == functionName {
		return fmt.Errorf("recursive call detected: %s", functionName)
	}
	ctx = slogcontext.WithValue(ctx, "function_name", functionName)
	ctx = slogcontext.WithValue(ctx, "alias", alias)

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
		var oe *smithy.OperationError
		var enf *types.ResourceNotFoundException
		if errors.As(err, &oe) {
			err = newHandlerError(err, http.StatusBadGateway)
		} else if errors.As(err, &enf) {
			err = newHandlerError(err, http.StatusNotFound)
		}
		return fmt.Errorf("failed to invoke: %w", err)
	}

	if resp.FunctionError != nil {
		return fmt.Errorf("function error: %s", *resp.FunctionError)
	}

	var res ridge.Response
	if err := json.Unmarshal(resp.Payload, &res); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	upstreamCode := res.StatusCode
	if _, err := res.WriteTo(w); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}
	slog.InfoContext(ctx, "handleProxy", "upstream_status", upstreamCode)

	return nil
}
