FROM golang:1.15-stretch AS builder

ENV CGO_ENABLED 0
ENV GOOS linux
ENV GOARCH amd64

WORKDIR /go/src/function-controller

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN BIN_OUTPUT_DIR=/bin make function-controller && \
    mkdir /kodata && \
    ls -lah hack && \
    mv .git/* /kodata/ && \
    rm -rf ${GOPATH} && \
    rm -rf ${HOME}/.cache

FROM scratch

COPY --from=builder /kodata/ ${KO_DATA_PATH}/
COPY --from=builder /bin/function-controller /
COPY licenses/ /licenses/

ENTRYPOINT ["/function-controller"]
