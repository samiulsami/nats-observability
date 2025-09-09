# Variables
REGISTRY = sami7786
IMAGE_NAME = $(REGISTRY)/nats-loadtest:latest
DEPLOYMENT_TEMPLATE = k8s/deployment.template.yaml
DEPLOYMENT_FILE = k8s/deployment.yaml
NAMESPACE = ace

# Load test configuration (can be overridden)
REPLICAS ?= 10
PRODUCERS ?= 200
CONSUMERS ?= 150
SLOW_CONSUMERS ?= 50

PRODUCER_RATE ?= 100
CONSUMER_RATE ?= 50
SLOW_CONSUMER_RATE ?= 20
MESSAGE_SIZE ?= 4096

# NATS configuration
NATS_URL ?= nats://ace-nats.ace.svc.cluster.local:4222
CREDS_PATH ?= /etc/nats/creds/admin.creds
SUBJECT ?= loadtest_$(shell date +"%Y%m%d_%H%M%S_%N")
STREAM_MAX_MSGS ?= 10000000
STREAM_MAX_BYTES ?= 2000000000

# Resource configuration
CPU_REQUESTS ?= 200m
MEMORY_REQUESTS ?= 256Mi
CPU_LIMITS ?= 1000m
MEMORY_LIMITS ?= 512Mi

.PHONY: deploy clean push build help generate-deployment

# Default target
help:
	@echo "Available commands:"
	@echo "  make deploy             - Delete deployment, build image, push image, apply deployment"
	@echo "  make clean              - Delete the deployment"
	@echo "  make push               - Push image to Docker registry"
	@echo "  make build              - Build Docker image"
	@echo "  make generate-deployment - Generate deployment.yaml from template"

# Build Docker image
build:
	@echo "Building Docker image..."
	docker build -t $(IMAGE_NAME) .

# Push image to Docker registry
push: build
	@echo "Pushing image to Docker registry..."
	docker push $(IMAGE_NAME)

# Generate deployment from template
generate-deployment:
	@echo "Generating deployment file from template..."
	IMAGE_NAME=$(IMAGE_NAME) \
	REPLICAS=$(REPLICAS) \
	PRODUCERS=$(PRODUCERS) \
	CONSUMERS=$(CONSUMERS) \
	SLOW_CONSUMERS=$(SLOW_CONSUMERS) \
	PRODUCER_RATE=$(PRODUCER_RATE) \
	CONSUMER_RATE=$(CONSUMER_RATE) \
	SLOW_CONSUMER_RATE=$(SLOW_CONSUMER_RATE) \
	MESSAGE_SIZE=$(MESSAGE_SIZE) \
	NATS_URL=$(NATS_URL) \
	CREDS_PATH=$(CREDS_PATH) \
	SUBJECT=$(SUBJECT) \
	STREAM_MAX_MSGS=$(STREAM_MAX_MSGS) \
	STREAM_MAX_BYTES=$(STREAM_MAX_BYTES) \
	CPU_REQUESTS=$(CPU_REQUESTS) \
	MEMORY_REQUESTS=$(MEMORY_REQUESTS) \
	CPU_LIMITS=$(CPU_LIMITS) \
	MEMORY_LIMITS=$(MEMORY_LIMITS) \
	envsubst < $(DEPLOYMENT_TEMPLATE) > $(DEPLOYMENT_FILE)

# Delete deployment
clean:
	@echo "Deleting deployment..."
	kubectl delete -f $(DEPLOYMENT_FILE) -n $(NAMESPACE) --ignore-not-found=true

# Full deploy: clean -> build -> push -> generate -> apply
deploy: clean push generate-deployment
	@echo "Applying deployment..."
	kubectl apply -f $(DEPLOYMENT_FILE) -n $(NAMESPACE)
	@echo "Deployment complete!"
