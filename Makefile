NAME    := sendgrid2datadog
SRCS    := $(shell find . -type f -name '*.go')
LDFLAGS := -ldflags="-s -w -extldflags \"-static\""

DIST_DIRS := find * -type d -exec

.DEFAULT_GOAL := bin/$(NAME)

bin/$(NAME): $(SRCS)
	go build $(LDFLAGS) -o bin/$(NAME)

.PHONY: clean
clean:
	rm -rf bin/*
	rm -rf vendor/*

.PHONY: cross-build
cross-build: deps
	for os in darwin linux windows; do \
		for arch in amd64 386; do \
			GOOS=$$os GOARCH=$$arch go build -a -tags netgo -installsuffix netgo $(LDFLAGS) -o dist/$$os-$$arch/$(NAME); \
		done; \
	done

.PHONY: deps
deps: glide
	glide install

.PHONY: dist
dist:
	cd dist && \
	$(DIST_DIRS) cp ../LICENSE {} \; && \
	$(DIST_DIRS) cp ../README.md {} \; && \
	$(DIST_DIRS) tar -zcf $(NAME)-$(VERSION)-{}.tar.gz {} \; && \
	$(DIST_DIRS) zip -r $(NAME)-$(VERSION)-{}.zip {} \; && \
	cd ..

.PHONY: glide
glide:
ifeq ($(shell command -v glide 2> /dev/null),)
	curl https://glide.sh/get | sh
endif

.PHONY: install
install:
	go install $(LDFLAGS)

.PHONY: test
test:
	go test -cover -v `glide novendor`

.PHONY: update-deps
update-deps: glide
	glide update
