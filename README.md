# Lamux

## Description

Lamux is a HTTP multiplexer for AWS Lambda Function aliases.

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

Lamux runs an HTTP server that listens on a specified port and forwards requests to a specified Lambda function aliases. The Lambda function alias is identified by its name, and the domain suffix is used to determine which requests should be forwarded to it.

For example, if you run `lamux` with `--function-name=example` and `--domain-suffix=example.com`, it will forward requests to `foo.example.com` to the Lambda function aliased `example:foo`.

The forwarded Lambda functions should process Function URLs payload, but these functions do not need Function URLs settings.

| Request URL | Lambda Function | Alias |
|-------------|-----------------|-------|
| `http://foo.example.com/` | `example` | `foo` |
| `http://bar.example.com/` | `example` | `bar` |

#### Limitations

Lambda alias names allow alphanumeric characters, hyphens, and underscores, but domain names do not allow underscores. And more, lamux uses `-` as a delimiter between the alias and the function name. Lamux requires `[a-zA-Z0-9-]+` as the alias name.

### Route to multiple Lambda functions

You can route requests to any Lambda function by specifying the `--function-name` set to `*`.

In this case, Lamux will forward requests to the Lambda function aliased `myalias-myfunc.example.com` to the Lambda function `myfunc` aliased as `myalias`.

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

## Installation

[Download the latest release](https://github.com/fujiwara/lamux/releases)

The `lamux` binary is a standalone executable. You can run it on your local machine or deploy it to AWS Lambda `bootstrap` for custom runtimes (provided.al2023 or provided.al2).

## Configuration

All settings can be specified via command-line flags or environment variables.

### `-port` (`$LAMUX_PORT`)

Port to listen on. Default is `8080`. This setting is ignored when `lamux` running on AWS Lambda Function URLs.

### `--function-name` (`$LAMUX_FUNCTION_NAME`)

Name of the Lambda function to proxy. This setting is required.

If you set `--function-name` to `*`, Lamux will route requests to any Lambda function. In this case, the Lambda function and alias are determined by the hostname.

### `--domain-suffix` (`$LAMUX_DOMAIN_SUFFIX`)

Domain suffix to accept requests for. This setting is required.

### `--upstream-timeout` (`$LAMUX_UPSTREAM_TIMEOUT`)

Timeout for upstream requests. Default is `30s`.

This setting is affected by the Lambda function timeout. If the Lambda function timeout is less than the `--upstream-timeout`, it will time out before the `--upstream-timeout`.


## LICENSE

MIT

## Author

Fujiwara Shunichiro
