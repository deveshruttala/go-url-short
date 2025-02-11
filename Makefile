cleanup:
	rm -f *-shm *-wal *.db

rebuild: cleanup
	go build -o bin/shortner ./cmd/cli/main.go
	./out/shortner seed -size 1M

containerise:
	podman build -t golang-server-with-memcached .
	podman run -p 9091:9091 -p 11211:11211 golang-server-with-memcached

tidy:
	go mod tidy

build.cli:
	go build -ldflags "-s -w" -o bin/cli ./cmd/cli/main.go

build.server:
	go build -ldflags "-s -w" -o bin/server ./cmd/server/main.go

build.all: tidy build.server build.cli
