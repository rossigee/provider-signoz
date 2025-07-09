FROM gcr.io/distroless/static:nonroot

ARG TARGETOS
ARG TARGETARCH

ADD bin/${TARGETOS}_${TARGETARCH}/provider /usr/local/bin/crossplane-signoz-provider

USER 65532
ENTRYPOINT ["crossplane-signoz-provider"]