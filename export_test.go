package lamux

type LambdaClient lambdaClient

func (l *Lamux) SetClient(client LambdaClient) {
	l.lambdaClient = client
}

func (l *Lamux) SetAccountID(accountID string) {
	l.accountID = accountID
}
