FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.title="provider-signoz"
LABEL org.opencontainers.image.description="Crossplane provider for SigNoz management"
LABEL org.opencontainers.image.vendor="rossigee"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.source="https://github.com/rossigee/provider-signoz"

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=0755 bin/${TARGETOS}_${TARGETARCH}/provider /usr/local/bin/provider

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/provider"]