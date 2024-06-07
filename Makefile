.PHONY: clean test

lamux: go.* *.go
	go build -o $@ cmd/lamux/main.go

clean:
	rm -rf lamux dist/

test:
	go test -v ./...

install:
	go install github.com/fujiwara/lamux/cmd/lamux

dist:
	goreleaser build --snapshot --rm-dist
