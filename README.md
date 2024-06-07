# Lamux

## Description

Lamux is a multiplexer for AWS Lambda Function aliases.

## Usage

```console
Usage: lamux --function-name=STRING --domain-suffix=STRING [flags]

Flags:
  -h, --help                    Show context-sensitive help.
      --port=8080               Port to listen on ($LAMUX_PORT)
      --function-name=STRING    Name of the Lambda function to proxy ($LAMUX_FUNCTION_NAME)
      --domain-suffix=STRING    Domain suffix to accept requests for ($LAMUX_DOMAIN_SUFFIX)
      --upstream-timeout=30s    Timeout for upstream requests ($LAMUX_UPSTREAM_TIMEOUT)
```

Lamux runs an HTTP server that listens on a specified port and forwards requests to a specified Lambda function. The Lambda function is identified by its name, and the domain suffix is used to determine which requests should be forwarded to it.

For example, if you run `lamux` with `--function-name=example` and `--domain-suffix=example.com`, it will forward requests to `foo.example.com` to the Lambda function aliased `example:foo`.

The forwarded Lambda functions should process Function URLs payload, but these functions do not need Function URLs settings.

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

## Installation

TODO

## Configuration

All settings can be specified via command-line flags or environment variables.

### `-port` (`$LAMUX_PORT`)

Port to listen on. Default is `8080`. This setting is ignored when running on AWS Lambda Function URLs.

### `--function-name` (`$LAMUX_FUNCTION_NAME`)

Name of the Lambda function to proxy. This setting is required.

### `--domain-suffix` (`$LAMUX_DOMAIN_SUFFIX`)

Domain suffix to accept requests for. This setting is required.

### `--upstream-timeout` (`$LAMUX_UPSTREAM_TIMEOUT`)

Timeout for upstream requests. Default is `30s`.


## LICENSE

MIT

## Author

Fujiwara Shunichiro
