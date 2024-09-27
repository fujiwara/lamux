package lamux

import (
	"context"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
)

type LambdaClient lambdaClient

func (l *Lamux) SetTestClient(client LambdaClient) {
	l.awsCfg = aws.Config{}
	l.SetAccountID("123456789012")
	l.lambdaClient = client
}

func (l *Lamux) HandleProxy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	return l.handleProxy(ctx, w, r)
}
