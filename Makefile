export

.SILENT: ;               # no need for @
.EXPORT_ALL_VARIABLES: ; # send all vars to shell

# Run make help by default
.DEFAULT_GOAL = .help

.PHONY: .help

build:
	docker build -t sam0delkin/dirsync:latest -f Dockerfile .


build_multi_arch:
	docker buildx build --push --platform=linux/amd64,linux/arm64 -t sam0delkin/dirsync:latest -f Dockerfile --build-arg BUILDKIT_INLINE_CACHE=1 .
