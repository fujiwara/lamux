package lamux

type LambdaClient lambdaClient

func (l *Lamux) SetClient(client LambdaClient) {
	l.lambdaClient = client
}
