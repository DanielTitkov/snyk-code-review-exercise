
.PHONY:
run:
	go run main.go

.PHONY:
test:
	go test -v ./... | tee result.log | tail -20
