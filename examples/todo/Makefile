# Run the todo example in a local kind cluster, and test it end to end.
# Everything builds and runs in containers: podman, kind and kubectl are the
# only tools needed on the host.
#
#   make up      cluster, images, deploy; then open http://localhost:8090
#   make e2e     browser test: an agent adds a to-do over MCP, the UI shows it live
#   make down    delete the cluster

CLUSTER          ?= todo-example
PORT             ?= 8090
CONTEXT          := kind-$(CLUSTER)
IMAGE            := localhost/todo-example:dev
WEB_IMAGE        := localhost/todo-example-web:dev
NODE_IMAGE       := docker.io/library/node:24-alpine
PLAYWRIGHT_IMAGE := mcr.microsoft.com/playwright:v1.63.0-noble
BUILD            := .build
REPO             := ../..
KUBECTL          := kubectl --context $(CONTEXT)
# Every base image is public. An empty credentials file keeps podman from
# sending stale stored registry credentials (such as expired gcr.io logins).
NO_AUTH          := $(BUILD)/no-auth.json

export KIND_EXPERIMENTAL_PROVIDER ?= podman

.PHONY: up down cluster images load deploy status logs e2e web-gen web-dev

## up: create the cluster if needed, build and load both images, deploy
up: cluster images load deploy
	@echo
	@echo "UI    http://localhost:$(PORT)"
	@echo "Docs  http://localhost:$(PORT)/docs"
	@echo "MCP   http://localhost:$(PORT)/mcp  (header: Authorization: Bearer writer)"

cluster:
	@kind get clusters 2>/dev/null | grep -qx $(CLUSTER) || { \
		mkdir -p $(BUILD) && sed 's/HOST_PORT/$(PORT)/' deploy/kind.yaml > $(BUILD)/kind.yaml && \
		kind create cluster --name $(CLUSTER) --config $(BUILD)/kind.yaml; }

## images: build the service (from the repository root) and the web UI
images: $(NO_AUTH)
	podman build --authfile $(NO_AUTH) -f Containerfile -t $(IMAGE) $(REPO)
	podman build --authfile $(NO_AUTH) -t $(WEB_IMAGE) web

$(NO_AUTH):
	mkdir -p $(BUILD)
	echo '{}' > $@

# kind load docker-image needs Docker; image archives work with podman.
load:
	mkdir -p $(BUILD)
	podman save -o $(BUILD)/todo.tar $(IMAGE)
	podman save -o $(BUILD)/web.tar $(WEB_IMAGE)
	kind load image-archive $(BUILD)/todo.tar --name $(CLUSTER)
	kind load image-archive $(BUILD)/web.tar --name $(CLUSTER)

deploy:
	$(KUBECTL) apply -f deploy/app.yaml
	$(KUBECTL) rollout restart deployment/todo deployment/web
	$(KUBECTL) rollout status deployment/todo --timeout=120s
	$(KUBECTL) rollout status deployment/web --timeout=120s

status:
	$(KUBECTL) get pods,services

logs:
	$(KUBECTL) logs deployment/todo --tail=50

## e2e: run the Playwright tests in a container on kind's network
e2e: $(NO_AUTH)
	podman run --rm --authfile $(NO_AUTH) --network kind --ipc=host \
		-v $(CURDIR)/e2e:/e2e -w /e2e \
		-e BASE_URL=http://$(CLUSTER)-control-plane:30080 \
		$(PLAYWRIGHT_IMAGE) sh -c "npm ci --no-audit --no-fund --loglevel=error && npx playwright test"

## web-gen: regenerate the TypeScript client with protoc-gen-es in a container
web-gen: web/node_modules
	buf generate --template buf.gen.web.yaml

web/node_modules: web/package.json web/package-lock.json $(NO_AUTH)
	podman run --rm --authfile $(NO_AUTH) -v $(CURDIR)/web:/app -w /app $(NODE_IMAGE) \
		npm ci --no-audit --no-fund --loglevel=error
	touch $@

## web-dev: Vite dev server in a container on :5173, proxying to the cluster
web-dev: $(NO_AUTH)
	podman run --rm -it --authfile $(NO_AUTH) -p 127.0.0.1:5173:5173 -v $(CURDIR)/web:/app -w /app \
		-e API_URL=http://host.containers.internal:$(PORT) \
		$(NODE_IMAGE) sh -c "npm ci --no-audit --no-fund --loglevel=error && npm run dev"

down:
	kind delete cluster --name $(CLUSTER)
