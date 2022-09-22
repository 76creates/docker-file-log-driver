# make version read latest git tag
VERSION = 1.0

.PHONY: all
all: mod build

.PHONY: mod
mod:
	go mod tidy
	go mod vendor

.PHONY: build
build:
	go build --ldflags '-extldflags "-static"' -o /usr/bin/docker-log-driver

.PHONY: plugin
plugin:
	# cleanup
	rm -rf ./rootfs
	rm -f container_fs.tar
	# build image
	docker build -t file-log-driver:v${VERSION} .
	# create container to harness root fs from && export filesystem
	docker export -o container_fs.tar `docker create file-log-driver:v${VERSION}`
	# untar to rootfs
	mkdir rootfs && tar -xf container_fs.tar --directory ./rootfs
	# create plugin
	docker plugin create file-log-driver:v${VERSION} .
	# enable plugin
	docker plugin enable file-log-driver:v${VERSION}
