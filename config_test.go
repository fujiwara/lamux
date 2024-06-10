package lamux_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/fujiwara/lamux"
)

type result struct {
	alias    string
	function string
}

type testCase struct {
	name   string
	cfg    *lamux.Config
	req    func() *http.Request
	expect result
}

var TestCasesOK = []testCase{
	{
		name: "fixed function name",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "myfunc",
			DomainSuffix:    "example.com",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://myalias.example.com", nil)
			req.Header.Set("Host", "example.com")
			return req
		},
		expect: result{
			alias:    "myalias",
			function: "myfunc",
		},
	},
	{
		name: "extract alias and function name",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "*",
			DomainSuffix:    "example.net",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://myalias-myfunc.example.net", nil)
			req.Header.Set("Host", "myalias-myfunc.example.net")
			return req
		},
		expect: result{
			alias:    "myalias",
			function: "myfunc",
		},
	},
	{
		name: "x-forwarded-host",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "myfunc",
			DomainSuffix:    "example.com",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://localhost:8080", nil)
			req.Header.Set("Host", "localhost:8080")
			req.Header.Set("X-Forwarded-Host", "myalias.example.com")
			return req
		},
		expect: result{
			alias:    "myalias",
			function: "myfunc",
		},
	},
}

var TestCasesNG = []testCase{
	{
		name: "fixed function name",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "myfunc",
			DomainSuffix:    "example.com",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://my-alias.example.com", nil)
			req.Header.Set("Host", "example.com")
			return req
		},
	},
	{
		name: "extract alias and function name",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "*",
			DomainSuffix:    "example.net",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://myalias-my-func.example.net", nil)
			req.Header.Set("Host", "example.com")
			return req
		},
	},
	{
		name: "invalid domain suffix",
		cfg: &lamux.Config{
			Port:            8080,
			FunctionName:    "*",
			DomainSuffix:    "example.net",
			UpstreamTimeout: 30,
		},
		req: func() *http.Request {
			req, _ := http.NewRequest("GET", "http://myalias-myfunc.example.com", nil)
			req.Header.Set("Host", "example.com")
			return req
		},
	},
}

func TestConfigOK(t *testing.T) {
	ctx := context.TODO()
	for _, c := range TestCasesOK {
		t.Run(c.name, func(t *testing.T) {
			_, err := lamux.NewLamux(c.cfg)
			if err != nil {
				t.Fatalf("failed to create Lamux: %v", err)
			}
			alias, function, err := c.cfg.ExtractAliasAndFunctionName(ctx, c.req())
			if err != nil {
				t.Fatalf("failed to extract alias and function: %v", err)
			}
			if alias != c.expect.alias {
				t.Fatalf("expected alias %q, got %q", c.expect.alias, alias)
			}
			if function != c.expect.function {
				t.Fatalf("expected function %q, got %q", c.expect.function, function)
			}
		})
	}
}

func TestConfigNG(t *testing.T) {
	ctx := context.TODO()
	for _, c := range TestCasesNG {
		t.Run(c.name, func(t *testing.T) {
			_, err := lamux.NewLamux(c.cfg)
			if err != nil {
				t.Fatalf("failed to create Lamux: %v", err)
			}
			_, _, err = c.cfg.ExtractAliasAndFunctionName(ctx, c.req())
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			t.Log(err)
		})
	}
}
