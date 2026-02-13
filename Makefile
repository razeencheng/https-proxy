

build:
	@echo "Building..."
	@GOOS=linux GOARCH=amd64 go build --ldflags '-w -s' -o https-proxy . 