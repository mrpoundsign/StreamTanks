FROM ubuntu:latest
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY ccserver-linux /ccserver
COPY ext-web/public /ext-web/public
RUN chmod +x /ccserver
ENTRYPOINT ["/ccserver", "-port", "8102"]
