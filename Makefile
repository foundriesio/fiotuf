TAGS:=${FIOTUF_TAGS}

COMMIT:=$(shell git log -1 --pretty=format:%h)$(shell git diff --quiet || echo '_')

# Use linker flags to provide commit info
LDFLAGS=-ldflags "-X=github.com/foundriesio/fiotuf/internal.Commit=$(COMMIT)"

TARGETS=bin/fiotuf-linux-amd64 bin/fiotuf-linux-arm

linter:=$(shell which golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)

build: $(TARGETS)
	@true

bin/fiotuf-linux-amd64:
bin/fiotuf-linux-armv7:
bin/fiotuf-linux-arm:
bin/fiotuf-%: FORCE
	CGO_ENABLED=0 \
	GOOS=$(shell echo $* | cut -f1 -d\- ) \
	GOARCH=$(shell echo $* | cut -f2 -d\-) \
		go build -o $@ -tags disable_pkcs11 main.go

# go build -tags vpn $(LDFLAGS) -o $@ main.go

FORCE:

format:
	@gofmt -l  -w ./

lint:
	@test -z $(shell gofmt -d -l ./ | tee /dev/stderr) || (echo "[WARN] Fix formatting issues with 'make format'"; exit 1)
	@test -x $(linter) || (echo "Please install linter from https://github.com/golangci/golangci-lint/releases/tag/v1.25.1 to $(HOME)/go/bin")
	$(linter) run --build-tags $(TAGS)

check: test lint

test:
	go test ./... -v
