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
		Payload:         []byte(fmt.Sprintf(`{"statusCode":%d}`, m.code)),
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
