FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/tertib ./cmd/tertib

FROM alpine:3.24
# git: tertib diffs the repo. ca-certificates: HTTPS to the model endpoint and AWS.
RUN apk add --no-cache git ca-certificates
# CI runners mount the repo owned by the host user, not the container user;
# without this git refuses to touch it ("dubious ownership").
ENV GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0=safe.directory \
    GIT_CONFIG_VALUE_0=*
COPY --from=build /out/tertib /usr/local/bin/tertib
WORKDIR /repo
ENTRYPOINT ["tertib"]
CMD ["check"]
