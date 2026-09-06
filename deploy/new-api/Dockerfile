FROM golang:1.26.1-alpine AS source
RUN apk add --no-cache git
WORKDIR /src
RUN git init && git remote add origin https://github.com/QuantumNous/new-api.git && git fetch --depth 1 origin f116414284162ad15d8925f7bca494c109b83e93 && git checkout --detach FETCH_HEAD
COPY strict.patch /strict.patch
RUN git apply --check /strict.patch && git apply /strict.patch
COPY model/strict_affinity.go.in model/strict_affinity.go
COPY middleware/strict_affinity.go.in middleware/strict_affinity.go
RUN go mod download

FROM oven/bun:1 AS web
WORKDIR /src/web
COPY --from=source /src/web/ ./
COPY --from=source /src/VERSION /src/VERSION
RUN bun install --frozen-lockfile && DISABLE_ESLINT_PLUGIN=true bun run build

FROM source AS build
ENV CGO_ENABLED=0 GOWORK=off
COPY --from=web /src/web/dist ./web/dist
RUN go build -ldflags '-s -w -X github.com/QuantumNous/new-api/common.Version=v1.0.0-rc.25-strict.1' -o /new-api .

FROM source AS unit
COPY model/strict_affinity_test.go.in model/strict_affinity_test.go
RUN go test ./model -run '^TestStrictBindingAtomicAndDurable$' -count=1

FROM calciumion/new-api:v1.0.0-rc.25
COPY --from=build /new-api /new-api
