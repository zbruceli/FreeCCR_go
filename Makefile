# FreeCCR-go build targets.
#
# RAW decode uses cgo + libraw (`brew install libraw`). libraw's pkg-config
# emits an OpenMP flag (-Xpreprocessor -fopenmp) that cgo rejects unless
# whitelisted, so the libraw targets export CGO_LDFLAGS_ALLOW.

BIN        := bin
LIBRAW_ENV := CGO_ENABLED=1 CGO_LDFLAGS_ALLOW='-Xpreprocessor|-fopenmp'
LIBRAW_TAG := -tags libraw

.PHONY: build build-raw serve serve-raw test test-raw golden vet fmt clean

## CLI + web server (standard formats only, no cgo).
build:
	go build -o $(BIN)/freeccr ./cmd/freeccr
	go build -o $(BIN)/freeccrd ./cmd/freeccrd

## CLI + web server with RAW/libraw support.
build-raw:
	$(LIBRAW_ENV) go build $(LIBRAW_TAG) -o $(BIN)/freeccr ./cmd/freeccr
	$(LIBRAW_ENV) go build $(LIBRAW_TAG) -o $(BIN)/freeccrd ./cmd/freeccrd

## Build (with RAW) and launch the local web UI.
serve: build-raw
	$(BIN)/freeccrd

test:
	go test ./...

## Tests including the libraw-tagged decode path. Set FREECCR_TEST_RAW=/path.raw
## to also run the bit-exact decode cross-check against dcraw_emu.
test-raw:
	$(LIBRAW_ENV) go test $(LIBRAW_TAG) ./...

## Regenerate golden fixtures from the numpy reference (needs python3 + numpy).
golden:
	python3 ref/gen_golden.py

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal ./tools

clean:
	rm -rf $(BIN)
