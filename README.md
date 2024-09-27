# Lamux

## Description

Lamux is a HTTP multiplexer for AWS Lambda Function aliases.

## Usage

```console
Usage: lamux [flags]

Flags:
  -h, --help                              Show context-sensitive help.
      --port=8080                         Port to listen on ($LAMUX_PORT)
      --function-name="*"                 Name of the Lambda function to proxy ($LAMUX_FUNCTION_NAME)
      --domain-suffix="localdomain"       Domain suffix to accept requests for ($LAMUX_DOMAIN_SUFFIX)
      --upstream-timeout=30s              Timeout for upstream requests ($LAMUX_UPSTREAM_TIMEOUT)
      --version                           Show version information
      --trace-insecure                    Disable TLS for Otel trace endpoint ($OTEL_EXPORTER_OTLP_INSECURE)
      --trace-protocol="http/protobuf"    Otel trace protocol ($OTEL_EXPORTER_OTLP_PROTOCOL)
      --trace-headers=KEY=VALUE;...       Additional headers for Otel trace endpoint (key1=value1;key2=value2)
                                          ($OTEL_EXPORTER_OTLP_HEADERS)
      --trace-service="lamux"             Service name for Otel trace ($OTEL_SERVICE_NAME)
      --trace-batch                       Enable batcher for Otel trace ($OTEL_EXPORTER_OTLP_BATCH)

traceOutput
  --trace-stdout             Enable stdout exporter for Otel trace ($OTEL_EXPORTER_STDOUT)
  --trace-endpoint=STRING    Otel trace endpoint (e.g. localhost:4318) ($OTEL_EXPORTER_OTLP_ENDPOINT)
```

Lamux runs an HTTP server that listens on a specified port and forwards requests to a specified Lambda function aliases. The Lambda function alias is identified by its name, and the domain suffix is used to determine which requests should be forwarded to it.

For example, if you run `lamux` with `--function-name=example` and `--domain-suffix=example.com`, it will forward requests to `foo.example.com` to the Lambda function aliased `example:foo`.

The forwarded Lambda functions should process Function URLs payload, but these functions do not need Function URLs settings.

| Request URL | Lambda Function | Alias |
|-------------|-----------------|-------|
| `http://foo.example.com/` | `example` | `foo` |
| `http://bar.example.com/` | `example` | `bar` |

#### Limitations

Lambda alias names allow alphanumeric characters, hyphens, and underscores, but domain names do not allow underscores. And more, lamux uses `-` as a delimiter between the alias and the function name.

- alias name pattern: `^[a-zA-Z0-9]+$` (`-` and `_` are not allowed)
- function name allows: `^[a-zA-Z0-9-]+$` (`-` is allowed, `_` is not allowed)

### Route to multiple Lambda functions

You can route requests to any Lambda function by specifying the `--function-name` set to `*`.

In this case, Lamux will forward requests to the Lambda function aliased `myalias-my-func.example.com` to the Lambda function `my-func` aliased as `myalias`.

| Request URL | Lambda Function | Alias |
|-------------|-----------------|-------|
| `http://foo-bar.example.com/` | `bar` | `foo` |
| `http://foo-baz.example.com/` | `baz` | `foo` |
| `http://bar-baz.example.com/` | `baz` | `bar` |

### Working with CloudFront and Lambda FunctionURLs

Lamux can work as a Lambda FunctionURLs. But in this case, Lamux cannot use the `Host` header because the Lambda function should be accessed via FunctionURLs (e.g., `***.lambda-url.us-east-1.on.aws`). So, Lamux uses the `X-Forwarded-Host` header to determine which requests should be forwarded to the Lambda function.

You may use CloudFront to forward requests to Lamux running on FunctionURLs. In this case, you should set the `X-Forwarded-Host` header to the original `Host` header value by Cloud Front Functions(CFF).

```js
// CloudFront Function for setting X-Forwarded-Host header in viewer request
async function handler(event) {
  const request = event.request;
  request.headers['x-forwarded-host'] = { value: request.headers['host'].value };
  return request;
}
```

### Working as a Lambda extension

Lamux can work as a Lambda extension. In this case, Lamux works the same as the local server mode, but it can be registered as a Lambda extension.

This mode is useful for calling other Lambda functions from the Lambda function running on a VPC without the NAT Gateway. Your Lambda handlers can invoke other Lambda functions by HTTP request to the Lamux extension.

To deploy Lamux as a Lambda extension, you need to create a Lambda layer that contains a `lamux` binary in the `extensions/` directory.

```console
$ mkdir extensions
$ cp /path/to/lamux extensions/lamux
$ zip -r layer.zip extensions
$ aws lambda publish-layer-version \
		--layer-name lamux \
		--zip-file fileb://layer.zip \
		--compatible-runtimes provided.al2023 provided.al2
```

## Installation

[Download the latest release](https://github.com/fujiwara/lamux/releases)

The `lamux` binary is a standalone executable. You can run it on your local machine or deploy it to AWS Lambda `bootstrap` for custom runtimes (provided.al2023 or provided.al2).

## Configuration

All settings can be specified via command-line flags or environment variables.

### `AWS_REGION` environment variable

AWS region to use.

### IAM Policy

Lamux must have the IAM policy, which can `lambda:InvokeFunction`.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "lambda:InvokeFunction",
      "Resource": "*",
    }
  ]
}
```

If you want to restrict the functions to invoke, you must set an IAM Policy to specify the Lambda function to be invoked.

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "lambda:InvokeFunction",
      "Resource": [
        "arn:aws:lambda:us-east-1:123456789012:function:foo:*",
        "arn:aws:lambda:us-east-1:123456789012:function:bar:*",
      ],
    }
  ]
}
```

If `lamux` runs on Lambda Function URLs, you should attach the appropriate execution policy to the Lambda function's role. (e.g., `AWSLambdaBasicExecutionRole` managed policy)

### `--port` (`$LAMUX_PORT`)

Port to listen on. Default is `8080`. This setting is ignored when `lamux` running on AWS Lambda Function URLs.

### `--function-name` (`$LAMUX_FUNCTION_NAME`)

Name of the Lambda function to proxy. This setting is required.

If you set `--function-name` to `*`, Lamux will route requests to any Lambda function. In this case, the Lambda function and alias are determined by the hostname.

### `--domain-suffix` (`$LAMUX_DOMAIN_SUFFIX`)

Domain suffix to accept requests for. This setting is required.

### `--upstream-timeout` (`$LAMUX_UPSTREAM_TIMEOUT`)

Timeout for upstream requests. Default is `30s`.

This setting is affected by the Lambda function timeout. If the Lambda function timeout is less than the `--upstream-timeout`, it will time out before the `--upstream-timeout`.


### OpenTelemetry tracing support

Lamux supports OpenTelemetry tracing.

When either of the following environment variables is set, Lamux will enable tracing.
- `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g., `localhost:4318`)
  - When you set this environment variable, Lamux will enable tracing and send traces to the specified endpoint.
- `OTEL_EXPORTER_STDOUT`
  - When you set this environment variable to `true`, Lamux will enable the stdout exporter for the trace.

Other optional environment variables for tracing:
- `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` or `http/protobuf`, default `http/protobuf`)
- `OTEL_EXPORTER_OTLP_INSECURE` (optional)
- `OTEL_EXPORTER_OTLP_HEADERS` (e.g., `key1=value1;key2=value2`)
- `OTEL_SERVICE_NAME` (default `lamux`)
- `OTEL_EXPORTER_OTLP_BATCH` (optional, default `false`)
  - When you set this environment variable to `true`, Lamux will enable the batcher for the trace exporter.
  - By default, the batcher is disabled, and the exporter sends traces synchronously. This is useful for running Lamux on Lambda Function URLs or debugging.
  - The batcher is useful for running Lamux on ECS tasks or EC2 instances (which means "long-running processes").

## LICENSE

MIT

## Author

Fujiwara Shunichiro
