# Image for the fake STS endpoint used by the e2e suite. It uses the same base
# images as the controller's Dockerfile so no extra registry is needed.
FROM golang:1.26 AS builder
WORKDIR /workspace
COPY main.go .
RUN CGO_ENABLED=0 go build -o fake-sts main.go

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /workspace/fake-sts /fake-sts
USER 65532:65532
ENTRYPOINT ["/fake-sts"]
