
.PHONY: run
run:
	go run main.go

.PHONY: test
test:
	go test -v ./... | tee result.log | tail -20

# low dependencies number
.PHONY: react
react:
	curl -s http://localhost:3000/package/react/16.13.0 | jq .

# high dependencies number (also with improper versions)
.PHONY: serverless
serverless:
	curl -s http://localhost:3000/package/serverless/4.10.1 | jq .
