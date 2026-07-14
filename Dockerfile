# syntax=docker/dockerfile:1

# ---- build driftlens ----
FROM golang:1.26-alpine AS builder
ARG VERSION=docker
ARG COMMIT=unknown
ARG DATE=unknown
WORKDIR /src
# Download modules first so they cache independently of source changes.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/driftlens ./cmd/driftlens

# ---- fetch terraform + terragrunt ----
FROM alpine:3.20 AS tools
ARG TERRAFORM_VERSION=1.9.8
ARG TERRAGRUNT_VERSION=1.1.0
ARG TARGETARCH
RUN apk add --no-cache curl unzip \
 && curl -fsSL -o /tmp/tf.zip \
      "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip" \
 && unzip /tmp/tf.zip -d /usr/local/bin \
 && curl -fsSL -o /usr/local/bin/terragrunt \
      "https://github.com/gruntwork-io/terragrunt/releases/download/v${TERRAGRUNT_VERSION}/terragrunt_linux_${TARGETARCH}" \
 && chmod +x /usr/local/bin/terraform /usr/local/bin/terragrunt

# ---- runtime ----
FROM alpine:3.20
# ca-certificates: provider/module downloads over HTTPS. git: git-sourced modules.
RUN apk add --no-cache ca-certificates git
COPY --from=tools /usr/local/bin/terraform /usr/local/bin/terraform
COPY --from=tools /usr/local/bin/terragrunt /usr/local/bin/terragrunt
COPY --from=builder /out/driftlens /usr/local/bin/driftlens
ENTRYPOINT ["driftlens"]
