package lamux_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
	"github.com/fujiwara/lamux"
)

type mockClient struct {
	code          int32
	body          string
	functionError *string
	latency       time.Duration
}

func (m *mockClient) Invoke(ctx context.Context, input *lambda.InvokeInput, optFns ...func(*lambda.Options)) (*lambda.InvokeOutput, error) {
	if aws.ToString(input.FunctionName) != "test-func" {
		return nil, &types.ResourceNotFoundException{
			Message: aws.String("Resource not found"),
		}
	}
	if aws.ToString(input.Qualifier) != "test" {
		return nil, &types.ResourceNotFoundException{
			Message: aws.String("Resource not found"),
		}
	}
	timer := time.NewTimer(m.latency)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if m.code >= 500 {
		return nil, &smithy.GenericAPIError{
			Code:    "InternalError",
			Message: "Internal server error",
		}
	}
	return &lambda.InvokeOutput{
		StatusCode:      m.code,
		FunctionError:   m.functionError,
		ExecutedVersion: aws.String("1"),
		LogResult:       aws.String("dummy"),
		Payload:         fmt.Appendf(nil, `{"statusCode":%d,"body":"%s"}`, m.code, m.body),
	}, nil
}

type clientTestCase struct {
	name         string
	client       *mockClient
	functionName string
	alias        string
	expectCode   int
}

var clientTestCases = []clientTestCase{
	{
		name: "invoke success",
		client: &mockClient{
			code: 200,
		},
		functionName: "test-func",
		alias:        "test",
		expectCode:   200,
	},
	{
		name: "invoke error not found function",
		client: &mockClient{
			code: 200,
		},
		functionName: "not-found",
		alias:        "test",
		expectCode:   404,
	},
	{
		name: "invoke error not found alias",
		client: &mockClient{
			code: 200,
		},
		functionName: "test-func",
		alias:        "not-found",
		expectCode:   404,
	},
	{
		name: "invoke error",
		client: &mockClient{
			code: 500,
		},
		functionName: "test-func",
		alias:        "test",
		expectCode:   502,
	},
	{
		name: "invoke timeout",
		client: &mockClient{
			code:    200,
			latency: 2 * time.Second,
		},
		functionName: "test-func",
		alias:        "test",
		expectCode:   504,
	},
	{
		name: "invoke function error",
		client: &mockClient{
			code:          405,
			functionError: aws.String("MethodNotAllowed"),
		},
		functionName: "test-func",
		alias:        "test",
		expectCode:   500,
	},
}

func TestClient(t *testing.T) {
	for _, tc := range clientTestCases {
		t.Run(tc.name, func(t *testing.T) {
			app, err := lamux.NewLamux(&lamux.Config{
				FunctionName:    "test-func",
				DomainSuffix:    "example.net",
				UpstreamTimeout: time.Second,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			app.SetTestClient(tc.client)

			var code int
			resp, err := app.Invoke(context.Background(), tc.functionName, tc.alias, nil)
			if err != nil {
				var herr *lamux.HandlerError
				if errors.As(err, &herr) {
					code = herr.Code()
				}
			} else {
				code = int(resp.StatusCode)
			}
			if e, a := tc.expectCode, code; e != int(a) {
				t.Errorf("expect %d, got %d", e, a)
			}
		})
	}
}

func TestProxy(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Host", "test.example.net")
	app, _ := lamux.NewLamux(&lamux.Config{
		FunctionName:    "test-func",
		DomainSuffix:    "example.net",
		UpstreamTimeout: time.Second,
	})
	app.SetTestClient(&mockClient{
		code: 200,
	})
	w := httptest.NewRecorder()
	if err := app.HandleProxy(context.Background(), w, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e, a := http.StatusOK, w.Code; e != a {
		t.Errorf("expect %d, got %d", e, a)
	}
}

type wrapHandlerTestCase struct {
	name           string
	noErrorDetails bool
	client         *mockClient
	alias          string
	expectCode     int
	expectBody     string
}

var wrapHandlerTestCases = []wrapHandlerTestCase{
	{
		name: "invoke success",
		client: &mockClient{
			code: 200,
			body: "OK",
		},
		alias:      "test",
		expectCode: 200,
		expectBody: "OK",
	},
	{
		name: "invoke error not found alias",
		client: &mockClient{
			code: 200,
			body: "OK",
		},
		alias:      "notfound",
		expectCode: 404,
		expectBody: "failed to invoke: ResourceNotFoundException: Resource not found\n",
	},
	{
		name: "invoke error not found alias with hide error",
		client: &mockClient{
			code: 200,
			body: "OK",
		},
		alias:          "notfound",
		noErrorDetails: true,
		expectCode:     404,
		expectBody:     "404 Not Found\n",
	},
}

func TestWrapHandler(t *testing.T) {
	for _, tc := range wrapHandlerTestCases {
		t.Run(tc.name, func(t *testing.T) {
			app, err := lamux.NewLamux(&lamux.Config{
				FunctionName:    "test-func",
				DomainSuffix:    "example.net",
				UpstreamTimeout: time.Second,
				ErrorDetails:    !tc.noErrorDetails,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			app.SetTestClient(tc.client)
			r, err := http.NewRequest("GET", "/", nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r.Header.Set("X-Forwarded-Host", fmt.Sprintf("%s.example.net", tc.alias))
			w := httptest.NewRecorder()
			handler := app.WrapHandler(app.HandleProxy)
			handler.ServeHTTP(w, r)
			if e, a := tc.expectCode, w.Code; e != a {
				t.Errorf("expect %d, got %d", e, a)
			}
			if e, a := tc.expectBody, w.Body.String(); e != a {
				t.Errorf("expect %s, got %s", e, a)
			}
		})
	}
}
