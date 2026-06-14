# Dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go test -v .
RUN go build -o server secrets.go main.go

FROM alpine:3.19

# Install jq for parsing Vault responses in entrypoint script
RUN apk add --no-cache jq ca-certificates bash

WORKDIR /root/
COPY --from=builder /app/server .
COPY entrypoint.sh .
RUN chmod +x entrypoint.sh

ENTRYPOINT ["./entrypoint.sh"]
CMD ["./server"]
